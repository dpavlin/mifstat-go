package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	OID_IFNAME = "1.3.6.1.2.1.31.1.1.1.1"
	OID_IFTYPE = "1.3.6.1.2.1.2.2.1.3"
	OID_HCIN   = "1.3.6.1.2.1.31.1.1.1.6"
	OID_HCOUT  = "1.3.6.1.2.1.31.1.1.1.10"
)

var metricOIDs = []string{OID_HCIN, OID_HCOUT}

// physIfTypes: SNMP ifType values we consider "real" interfaces worth tracking.
var physIfTypes = map[int]bool{6: true, 161: true}

func pollSwitch(sw *SwitchData, delay float64, timeout time.Duration, sem chan struct{}, slowMs int64) {
	sw.mu.RLock()
	mrep := sw.MaxRepetitions
	sw.mu.RUnlock()
	if mrep == 0 {
		mrep = 20
	}

	conn := &gosnmp.GoSNMP{
		Target:         sw.IP,
		Port:           161,
		Community:      snmpCommunity,
		Version:        gosnmp.Version2c,
		Timeout:        timeout,
		Retries:        0,
		MaxRepetitions: mrep,
	}

	for {
		t0 := time.Now()
		sem <- struct{}{}
		if err := conn.Connect(); err != nil {
			<-sem
			sw.mu.Lock()
			sw.Status = "CONN_ERR"
			sw.ErrorCount++
			sw.mu.Unlock()
			time.Sleep(time.Duration(delay * float64(time.Second)))
			continue
		}

		sw.mu.RLock()
		curMaxRep := sw.MaxRepetitions
		sw.mu.RUnlock()
		conn.MaxRepetitions = curMaxRep

		tables, err := bulkWalkMulti(conn, metricOIDs, curMaxRep)
		if err == nil && sw.PhysPorts == 0 {
			if pdus, errWalk := conn.BulkWalkAll(OID_IFNAME); errWalk == nil {
				sw.mu.Lock()
				sw.IfaceCount = len(pdus)
				sw.mu.Unlock()
			} else {
				err = errWalk
			}
			if err == nil {
				if pdus, errWalk := conn.BulkWalkAll(OID_IFTYPE); errWalk == nil {
					portTypes := map[int]int{}
					for _, pdu := range pdus {
						if idx, ok := oidIndex(pdu.Name, OID_IFTYPE); ok {
							portTypes[idx] = int(snmpUint64(pdu))
						}
					}
					sw.mu.Lock()
					sw.PhysPorts = countPhys(portTypes)
					// Optimize MaxRepetitions:
					if sw.PhysPorts > 0 {
						targetRep := uint32(sw.PhysPorts + 2)
						if targetRep > 50 {
							targetRep = 50
						}
						if targetRep < 5 {
							targetRep = 5
						}
						sw.MaxRepetitions = targetRep
					}
					sw.mu.Unlock()
				} else {
					err = errWalk
				}
			}
		}

		conn.Conn.Close()
		<-sem

		dur := time.Since(t0)
		sw.mu.Lock()
		sw.LastPollMs = dur.Milliseconds()
		sw.TotalPollMs += sw.LastPollMs
		sw.PollCount++

		if err != nil {
			sw.Status = "WALK_ERR"
			sw.ErrorCount++
		} else {
			if len(tables[OID_HCIN]) == 0 {
				sw.Status = "PARTIAL"
				sw.ErrorCount++
			} else {
				sw.Status = "OK"
			}
			now := float64(t0.UnixNano()) / 1e9
			processSamples(sw, tables, now, delay)
		}
		sw.mu.Unlock()

		if slowMs > 0 && dur.Milliseconds() > slowMs {
			logger.Printf("SLOW: %s (%s) took %v", sw.Name, sw.IP, dur)
		}

		// Adaptive delay: ensure we don't poll faster than the switch responds.
		// Wait at least the global delay, but also at least 1.2x the last response time.
		targetPeriod := math.Max(delay, (float64(dur.Milliseconds())/1000.0)*1.2)
		targetPeriod = math.Min(targetPeriod, 60.0)
		
		sleepFor := time.Duration(targetPeriod*float64(time.Second)) - dur
		if sleepFor > 0 {
			time.Sleep(sleepFor)
		}
	}
}

func processSamples(sw *SwitchData, tables map[string]map[int]uint64, now, delay float64) {
	inT := tables[OID_HCIN]
	outT := tables[OID_HCOUT]

	if sw.prevCounters == nil {
		sw.prevCounters = make(map[string][2]uint64)
	}
	if sw.Rates == nil {
		sw.Rates = make(map[string]*PortRate)
	}
	if sw.PortHist == nil {
		sw.PortHist = make(map[string]*PortHistory)
	}

	// Calculate actual dt from previous poll
	var dt float64
	if !sw.prevTS.IsZero() {
		dt = now - (float64(sw.prevTS.UnixNano()) / 1e9)
		if dt <= 0 {
			dt = delay
		}
		// Update SampleInterval EMA
		sw.SampleInterval = 0.9*sw.SampleInterval + 0.1*dt
	} else {
		dt = delay
	}

	firstPoll := sw.prevTS.IsZero()
	sw.prevTS = time.Unix(0, int64(now*1e9))

	totalIn, totalOut := 0.0, 0.0
	hasHistory := false
	for idx, cin := range inT {
		cout, ok := outT[idx]
		if !ok {
			continue
		}
		pname := fmt.Sprintf("p%d", idx)
		prev, exists := sw.prevCounters[pname]
		if exists && !firstPoll {
			// Calculate rates in KB/s using actual dt
			rin := float64(cin-prev[0]) / 1024.0 / dt
			rout := float64(cout-prev[1]) / 1024.0 / dt

			// Handle counter resets (reboots) - 64-bit HC wrap is extremely unlikely between polls
			if cin < prev[0] || cout < prev[1] {
				rin, rout = 0, 0
			}

			if sw.Rates[pname] == nil {
				sw.Rates[pname] = &PortRate{}
			}
			sw.Rates[pname].In = rin
			sw.Rates[pname].Out = rout
			sw.Rates[pname].EmaIn = calcEMA(rin, sw.Rates[pname].EmaIn, 0.1)
			sw.Rates[pname].EmaOut = calcEMA(rout, sw.Rates[pname].EmaOut, 0.1)
			if rin > sw.Rates[pname].MaxIn {
				sw.Rates[pname].MaxIn = rin
			}
			if rout > sw.Rates[pname].MaxOut {
				sw.Rates[pname].MaxOut = rout
			}

			totalIn += rin
			totalOut += rout
			hasHistory = true

			if sw.PortHist[pname] == nil {
				sw.PortHist[pname] = &PortHistory{}
			}
			sw.PortHist[pname].In = append(sw.PortHist[pname].In, Sample{now, rin})
			sw.PortHist[pname].Out = append(sw.PortHist[pname].Out, Sample{now, rout})
			// Prune
			sw.PortHist[pname].In = pruneSamples(sw.PortHist[pname].In, now)
			sw.PortHist[pname].Out = pruneSamples(sw.PortHist[pname].Out, now)
		}
		sw.prevCounters[pname] = [2]uint64{cin, cout}
	}

	if firstPoll || !hasHistory {
		return
	}

	sw.In, sw.Out = totalIn, totalOut
	sw.EmaIn = calcEMA(totalIn, sw.EmaIn, 0.1)
	sw.EmaOut = calcEMA(totalOut, sw.EmaOut, 0.1)
	if totalIn > sw.MaxIn {
		sw.MaxIn = totalIn
	}
	if totalOut > sw.MaxOut {
		sw.MaxOut = totalOut
	}
	sw.HistIn = append(sw.HistIn, Sample{now, totalIn})
	sw.HistOut = append(sw.HistOut, Sample{now, totalOut})
	sw.HistIn = pruneSamples(sw.HistIn, now)
	sw.HistOut = pruneSamples(sw.HistOut, now)
}

func pruneSamples(s []Sample, now float64) []Sample {
	cutoff := now - MAX_HIST_SEC
	i := 0
	for ; i < len(s); i++ {
		if s[i].TS >= cutoff {
			break
		}
	}
	if i > 0 {
		return s[i:]
	}
	return s
}

type SnmpClient interface {
	GetBulk(oids []string, nonRepeaters uint8, maxRepetitions uint32) (*gosnmp.SnmpPacket, error)
}

func bulkWalkMulti(conn SnmpClient, baseOIDs []string, maxRep uint32) (map[string]map[int]uint64, error) {
	n := len(baseOIDs)
	result := make(map[string]map[int]uint64, n)
	prefixes := make([]string, n)
	for i, oid := range baseOIDs {
		result[oid] = make(map[int]uint64)
		prefixes[i] = oid + "."
		if !strings.Contains(oid, ".") {
			prefixes[i] = "." + oid + "."
		}
	}

	current := make([]string, n)
	copy(current, baseOIDs)

	for {
		resp, err := conn.GetBulk(current, 0, maxRep)
		if err != nil || resp == nil {
			// If we haven't collected anything across ANY column yet, return the error.
			// (Check OID_HCIN as a proxy for 'anything collected')
			if len(result[baseOIDs[0]]) == 0 {
				return result, err
			}
			break
		}
		vars := resp.Variables
		if len(vars) == 0 {
			break
		}

		got := 0
		done := false
		// Responses are interleaved: vars[row*n+col] = column `col` for row `row`
		for row := 0; (row+1)*n <= len(vars); row++ {
			base := row * n
			// Stop if any column has left its subtree
			allOK := true
			for col := 0; col < n; col++ {
				pdu := vars[base+col]
				if pdu.Type == gosnmp.EndOfMibView ||
					pdu.Type == gosnmp.NoSuchObject ||
					pdu.Type == gosnmp.NoSuchInstance {
					allOK = false
					done = true
					break
				}
				// check both forms (with and without leading dot)
				p := pdu.Name
				if !strings.HasPrefix(p, prefixes[col]) &&
					!strings.HasPrefix("."+p, "."+prefixes[col]) &&
					!strings.HasPrefix(p, "."+prefixes[col]) {
					allOK = false
					done = true
					break
				}
			}
			if !allOK {
				break
			}
			for col, oid := range baseOIDs {
				pdu := vars[base+col]
				if idx, ok := oidIndex(pdu.Name, oid); ok {
					result[oid][idx] = snmpUint64(pdu)
				}
			}
			got++
		}

		if done || got == 0 || got < int(maxRep) {
			break
		}
		// Advance all column cursors to the last returned OID in each column
		for col := 0; col < n; col++ {
			current[col] = vars[(got-1)*n+col].Name
		}
	}
	return result, nil
}

func oidIndex(oid, base string) (int, bool) {
	oid = strings.TrimPrefix(oid, ".")
	base = strings.TrimPrefix(base, ".")
	prefix := base + "."
	if !strings.HasPrefix(oid, prefix) {
		return 0, false
	}
	suffix := oid[len(prefix):]
	n, err := strconv.Atoi(strings.SplitN(suffix, ".", 2)[0])
	return n, err == nil
}

func snmpUint64(pdu gosnmp.SnmpPDU) uint64 {
	switch v := pdu.Value.(type) {
	case uint:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	case int:
		return uint64(v)
	}
	return 0
}

func countPhys(m map[int]int) int {
	n := 0
	for _, t := range m {
		if physIfTypes[t] {
			n++
		}
	}
	return n
}
