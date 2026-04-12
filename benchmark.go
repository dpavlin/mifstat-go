package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// runBenchmark polls all switches multiple times concurrently and prints a timing table.
func runBenchmark(switches []map[string]string, sem chan struct{}, slowMs int64) {
	const iterations = 5
	type stats struct {
		min, max, total int64
		samples         []int64
	}
	type Result struct {
		IP, Name   string
		Status     string
		Snmp       stats
		SemWait    stats
		PhysPorts  int
		IfaceCount int
		OIDRows    int
		MaxRep     uint32
		SlowCount  int
	}

	results := make([]Result, len(switches))
	var wg sync.WaitGroup
	wallStart := time.Now()

	for i, sw := range switches {
		wg.Add(1)
		go func(i int, sw map[string]string) {
			defer wg.Done()
			r := Result{IP: sw["ip"], Name: sw["name"], Status: "OK"}
			r.Snmp.min = 99999
			r.SemWait.min = 99999

			for iter := 0; iter < iterations; iter++ {
				t0 := time.Now()
				sem <- struct{}{}
				waitMs := time.Since(t0).Milliseconds()
				r.SemWait.total += waitMs
				r.SemWait.samples = append(r.SemWait.samples, waitMs)
				if waitMs < r.SemWait.min { r.SemWait.min = waitMs }
				if waitMs > r.SemWait.max { r.SemWait.max = waitMs }

				tSnmp := time.Now()
				conn := &gosnmp.GoSNMP{
					Target:         sw["ip"],
					Port:           161,
					Community:      snmpCommunity,
					Version:        gosnmp.Version2c,
					Timeout:        3 * time.Second,
					Retries:        0,
					MaxRepetitions: 20,
				}
				if err := conn.Connect(); err != nil {
					r.Status = "CONN_ERR"
					<-sem
					break
				}

				if iter == 0 {
					pdus, err := conn.BulkWalkAll(OID_IFNAME)
					if err != nil {
						r.Status = "WALK_ERR"
						conn.Conn.Close()
						<-sem
						break
					}
					r.IfaceCount = len(pdus)
					pdus, err = conn.BulkWalkAll(OID_IFTYPE)
					if err != nil {
						r.Status = "WALK_ERR"
						conn.Conn.Close()
						<-sem
						break
					}
					portTypes := map[int]int{}
					for _, pdu := range pdus {
						if idx, ok := oidIndex(pdu.Name, OID_IFTYPE); ok {
							portTypes[idx] = int(snmpUint64(pdu))
						}
					}
					r.PhysPorts = countPhys(portTypes)
					
					// Optimize MaxRep
					r.MaxRep = uint32(r.PhysPorts + 2)
					if r.MaxRep > 50 { r.MaxRep = 50 }
					if r.MaxRep < 5 { r.MaxRep = 5 }
				}

				conn.MaxRepetitions = r.MaxRep
				tables, err := bulkWalkMulti(conn, metricOIDs, r.MaxRep)
				conn.Conn.Close()
				<-sem

				if err != nil {
					r.Status = "WALK_ERR"
					break
				}
				
				if len(tables[OID_HCIN]) == 0 {
					r.Status = "PARTIAL"
					break
				}
				
				snmpMs := time.Since(tSnmp).Milliseconds()
				r.Snmp.total += snmpMs
				r.Snmp.samples = append(r.Snmp.samples, snmpMs)
				if snmpMs < r.Snmp.min { r.Snmp.min = snmpMs }
				if snmpMs > r.Snmp.max { r.Snmp.max = snmpMs }
				r.OIDRows = len(tables[OID_HCIN])
				
				if slowMs > 0 && snmpMs > slowMs {
					r.SlowCount++
				}
				
				if iter < iterations-1 {
					time.Sleep(100 * time.Millisecond)
				}
			}
			results[i] = r
		}(i, sw)
	}
	wg.Wait()
	wall := time.Since(wallStart)

	sort.Slice(results, func(i, j int) bool { return results[i].Snmp.total > results[j].Snmp.total })

	bwIP, bwName, bwStatus := 12, 15, 8
	for _, r := range results {
		if len(r.IP) > bwIP { bwIP = len(r.IP) }
		if len(r.Name) > bwName { bwName = len(r.Name) }
	}

	fmt.Printf("Benchmark: %d switches | samples: %d | wall: %v | semaphore: 50 | slow threshold: %dms\n\n",
		len(switches), iterations, wall.Round(time.Millisecond), slowMs)
	fmt.Printf("%-*s %-*s %-*s %7s %7s %7s %5s %6s %4s %4s\n",
		bwIP, "IP", bwName, "Name", bwStatus, "Status",
		"AvgMs", "MaxMs", "Jitter", "Phys", "Ifaces", "MRep", "Slow")
	fmt.Println(strings.Repeat("-", bwIP+bwName+bwStatus+1+7+1+7+1+7+1+5+1+6+1+4+1+4+7))

	for _, r := range results {
		avg := r.Snmp.total / iterations
		jitter := r.Snmp.max - r.Snmp.min
		fmt.Printf("%-*s %-*s %-*s %7d %7d %7d %5d %6d %4d %4d\n",
			bwIP, r.IP, bwName, r.Name, bwStatus, r.Status,
			avg, r.Snmp.max, jitter, r.PhysPorts, r.IfaceCount, r.MaxRep, r.SlowCount)
	}
}
