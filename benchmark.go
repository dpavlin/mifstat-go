package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// runBenchmark polls all switches once concurrently and prints a timing table.
func runBenchmark(switches []map[string]string, sem chan struct{}) {
	type Result struct {
		IP, Name   string
		Status     string
		SemWaitMs  int64 // time waiting for semaphore slot
		SnmpMs     int64 // actual SNMP work time (connect + walk)
		PhysPorts  int
		IfaceCount int
		OIDRows    int
		OIDErrors  int
	}

	results := make([]Result, len(switches))
	var wg sync.WaitGroup
	wallStart := time.Now()

	for i, sw := range switches {
		wg.Add(1)
		go func(i int, sw map[string]string) {
			defer wg.Done()
			r := Result{IP: sw["ip"], Name: sw["name"]}

			t0 := time.Now()
			sem <- struct{}{}
			r.SemWaitMs = time.Since(t0).Milliseconds()

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
				r.SnmpMs = time.Since(tSnmp).Milliseconds()
				<-sem
				results[i] = r
				return
			}

			portTypes := map[int]int{}
			if pdus, err := conn.BulkWalkAll(OID_IFNAME); err == nil {
				r.IfaceCount = len(pdus)
			}
			if pdus, err := conn.BulkWalkAll(OID_IFTYPE); err == nil {
				for _, pdu := range pdus {
					if idx, ok := oidIndex(pdu.Name, OID_IFTYPE); ok {
						portTypes[idx] = int(snmpUint64(pdu))
					}
				}
				r.PhysPorts = countPhys(portTypes)
			}

			tables, _ := bulkWalkMulti(conn, metricOIDs, 20)
			conn.Conn.Close()
			<-sem

			r.SnmpMs = time.Since(tSnmp).Milliseconds()
			if rows := len(tables[OID_HCIN]); rows == 0 {
				r.OIDErrors++
			} else {
				r.OIDRows = rows
			}
			r.Status = "OK"
			if r.OIDErrors > 0 {
				r.Status = fmt.Sprintf("PARTIAL(%d)", r.OIDErrors)
			}
			results[i] = r
		}(i, sw)
	}
	wg.Wait()
	wall := time.Since(wallStart)

	sort.Slice(results, func(i, j int) bool { return results[i].SnmpMs > results[j].SnmpMs })

	// Dynamic column widths for bench output.
	bwIP, bwName, bwStatus := len("IP"), len("Name"), len("Status")
	for _, r := range results {
		if l := len(r.IP); l > bwIP {
			bwIP = l
		}
		if l := len(r.Name); l > bwName {
			bwName = l
		}
		if l := len(r.Status); l > bwStatus {
			bwStatus = l
		}
	}
	sepW := bwIP + 1 + bwName + 1 + bwStatus + 1 + 7 + 1 + 7 + 1 + 5 + 1 + 6 + 1 + 7

	fmt.Printf("Benchmark: %d switches | wall: %v | semaphore: 50 | multi-OID GETBULK\n\n",
		len(switches), wall.Round(time.Millisecond))
	fmt.Printf("%-*s %-*s %-*s %7s %7s %5s %6s %7s\n",
		bwIP, "IP", bwName, "Name", bwStatus, "Status",
		"SnmpMs", "SemMs", "Phys", "Ifaces", "HCinRows")
	fmt.Println(strings.Repeat("-", sepW))

	okCount, errCount := 0, 0
	var totalSnmp, maxSnmp, totalSem, maxSem int64
	for _, r := range results {
		if r.Status == "OK" {
			okCount++
		} else {
			errCount++
		}
		totalSnmp += r.SnmpMs
		totalSem += r.SemWaitMs
		if r.SnmpMs > maxSnmp {
			maxSnmp = r.SnmpMs
		}
		if r.SemWaitMs > maxSem {
			maxSem = r.SemWaitMs
		}
		fmt.Printf("%-*s %-*s %-*s %7d %7d %5d %6d %7d\n",
			bwIP, r.IP, bwName, r.Name, bwStatus, r.Status,
			r.SnmpMs, r.SemWaitMs, r.PhysPorts, r.IfaceCount, r.OIDRows)
	}
	fmt.Println(strings.Repeat("-", sepW))
	n := int64(len(results))
	fmt.Printf("Summary: %d OK, %d failed | snmp avg/max: %d/%dms | sem avg/max: %d/%dms | wall: %v\n",
		okCount, errCount,
		totalSnmp/n, maxSnmp,
		totalSem/n, maxSem,
		wall.Round(time.Millisecond))
}
