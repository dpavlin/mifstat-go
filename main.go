// mifstat — Multi-switch SNMP bandwidth monitor (Go single-binary)
// Build: cd mifstat-go && go mod tidy && CGO_ENABLED=0 go build -o ../mifstat-bin .
// Deploy: scp mifstat-bin root@black:/usr/local/bin/mifstat
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/gdamore/tcell/v2"
	"github.com/gosnmp/gosnmp"
)

var snmpCommunity = getCommunity()

func getCommunity() string {
	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".config", "snmp.community")
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return "public"
}

const (
	STATE_FILE_BIN   = "/tmp/mifstat_go.bin"
	MAX_HIST_SEC     = 21600.0

	OID_IFNAME  = "1.3.6.1.2.1.31.1.1.1.1"
	OID_IFTYPE  = "1.3.6.1.2.1.2.2.1.3"
	OID_HCIN    = "1.3.6.1.2.1.31.1.1.1.6"
	OID_HCOUT   = "1.3.6.1.2.1.31.1.1.1.10"
)

var metricOIDs = []string{OID_HCIN, OID_HCOUT}
var zoomLevels = []int{1, 2, 5, 10, 30, 60, 120}

// physIfTypes: SNMP ifType values we consider "real" interfaces worth tracking.
// 6=ethernetCsmacd, 161=ieee8023adLag (port-channel/LAG)
var physIfTypes = map[int]bool{6: true, 161: true}

var logger = log.New(io.Discard, "", 0) // replaced by initLogger if -log is set

func initLogger(path string) (close func()) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open log %s: %v\n", path, err)
		return func() {}
	}
	logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	logger.Printf("=== mifstat started ===")
	return func() { f.Close() }
}

// Sample is a (timestamp, value) data point for history sparklines.
type Sample struct{ TS, Val float64 }

type PortHistory struct{ In, Out []Sample }

type PortRate struct {
	In, Out float64
}

type SwitchData struct {
	mu           sync.RWMutex
	Name, IP     string
	Status       string
	In, Out      float64
	HistIn       []Sample
	HistOut      []Sample
	PortHist     map[string]*PortHistory
	Rates        map[string]*PortRate
	prevCounters map[string][2]uint64
	prevTS       time.Time

	// Performance metrics (not persisted)
	PollCount      uint64
	ErrorCount     uint64
	LastPollMs     int64
	TotalPollMs    int64
	PhysPorts      int     // ethernetCsmacd + LAG interfaces
	IfaceCount     int     // all SNMP interfaces
	SampleInterval float64 // EMA of actual inter-sample time (seconds)
}

// SaveState holds only the serialisable history (no mutexes or counters).
type SaveState struct {
	HistIn   map[string][]Sample
	HistOut  map[string][]Sample
	PortHist map[string]map[string]*PortHistory
}

// ===== Sparkline & header =====

// getSparkline renders history as block-character sparkline cells.
// Returns (chars, staleFlags): stale cells should be rendered with dimStyle.
// Both fresh and stale pixels use the same height-encoded block chars (▁-█),
// so magnitude is always visible. The age indicator is embedded at the end.
//
// Bug fixes vs previous string version:
//   - high==low && val>0 → idx=4 (mid-height), not 7. Prevents flat-traffic
//     switches from filling the entire sparkline with full-height █ blocks.
//   - Stale pixels render as the same block char (dimmed), not ░ which has no height.
func getSparkline(history []Sample, width int, delay float64, zoom int, now, sampleInterval float64) (chars []rune, staleFlags []bool) {
	chars = make([]rune, width)
	staleFlags = make([]bool, width)
	for i := range chars {
		chars[i] = ' '
	}
	if width <= 0 || len(history) == 0 {
		return
	}

	pixelSec := delay * float64(zoom)
	startTime := now - float64(width)*pixelSec

	// Build buckets: aligned time slot → max value in that slot.
	buckets := make(map[float64]float64)
	for _, s := range history {
		if s.TS < startTime {
			continue
		}
		tb := math.Floor(s.TS/pixelSec) * pixelSec
		if v, ok := buckets[tb]; !ok || s.Val > v {
			buckets[tb] = s.Val
		}
	}
	if len(buckets) == 0 {
		return
	}

	// Find rightmost real sample in the window.
	var lastSampleT float64
	for t := range buckets {
		if t > lastSampleT {
			lastSampleT = t
		}
	}

	// persistSec: how far forward to carry a sample value to fill inter-sample gaps.
	// Capped at the full window so we never leave blank history.
	effectivePeriod := math.Max(delay, sampleInterval)
	persistSec := math.Min(effectivePeriod*2.5*float64(zoom), float64(width)*pixelSec)

	data  := make([]float64, width)
	valid := make([]bool, width)
	stale := make([]bool, width) // true = pixel is in the right-edge gap

	for i := 0; i < width; i++ {
		tp := now - float64(width-1-i)*pixelSec
		tb := math.Floor(tp/pixelSec) * pixelSec
		if v, ok := buckets[tb]; ok {
			data[i], valid[i] = v, true
		} else {
			// Find most recent sample strictly before this pixel.
			var lastT, lastV float64
			found := false
			for t, v := range buckets {
				if t < tp && (!found || t > lastT) {
					lastT, lastV, found = t, v, true
				}
			}
			if found && (tp-lastT) < persistSec {
				data[i], valid[i] = lastV, true
				// Stale: this pixel is after the last real sample.
				if tb > lastSampleT {
					stale[i] = true
				}
			}
		}
	}

	// Normalise across all valid pixels (fresh and stale share the same scale).
	high, low := 1.0, 0.0
	first := true
	for i, v := range data {
		if !valid[i] {
			continue
		}
		if first {
			high, low, first = v, v, false
		} else {
			if v > high {
				high = v
			}
			if v < low {
				low = v
			}
		}
	}

	// Age indicator: how many seconds since the last real sample.
	ageS := int(now - lastSampleT)
	var ageStr string
	if ageS >= 3600 {
		ageStr = fmt.Sprintf("%2dh", ageS/3600)
	} else if ageS >= 60 {
		ageStr = fmt.Sprintf("%2dm", ageS/60)
	} else {
		ageStr = fmt.Sprintf("%2ds", ageS)
	}
	sparkW := width - len(ageStr)
	if sparkW < 0 {
		sparkW = 0
	}

	blockChars := []rune(" ▂▃▄▅▆▇█")
	for i := 0; i < sparkW; i++ {
		if !valid[i] {
			continue // leave as space
		}
		idx := 0
		if high > low {
			idx = int(((data[i] - low) / (high - low)) * 7)
		} else if data[i] > 0 {
			idx = 4 // flat non-zero traffic: mid-height; avoids misleading all-max bands
		}
		if idx > 7 {
			idx = 7
		}
		if idx < 0 {
			idx = 0
		}
		chars[i] = blockChars[idx]
		staleFlags[i] = stale[i] // stale cells rendered dim by caller; same char, less bright
	}
	// Embed age indicator at end (staleFlag stays false → rendered dim by convention below).
	for k, r := range []rune(ageStr) {
		if sparkW+k < width {
			chars[sparkW+k] = r
			staleFlags[sparkW+k] = true // dim the age text too (it's metadata, not data)
		}
	}
	return
}

func getTrendHeader(width int, delay float64, zoom int, viewNow float64) string {
	if width <= 0 {
		return ""
	}
	header := []rune(strings.Repeat(" ", width))
	pixelSec := delay * float64(zoom)
	totalSec := int(float64(width) * pixelSec)
	intervals := []int{1, 2, 5, 10, 30, 60, 300, 600, 1800, 3600, 7200, 14400, 28800}
	interval := 300
	for _, iv := range intervals {
		if totalSec/iv >= 2 {
			interval = iv
		} else {
			break
		}
	}
	for sec := 0; sec <= totalSec; sec += interval {
		var label string
		var pos int
		if sec == 0 {
			if time.Now().Unix()-int64(viewNow) < int64(pixelSec) {
				label = "Now"
			} else {
				label = time.Unix(int64(viewNow), 0).Format("15:04:05")
			}
			pos = width - len(label)
		} else {
			switch {
			case sec >= 3600:
				label = fmt.Sprintf("-%dh", sec/3600)
			case sec >= 60:
				label = fmt.Sprintf("-%dm", sec/60)
			default:
				label = fmt.Sprintf("-%ds", sec)
			}
			pos = width - int(float64(sec)/pixelSec) - len(label)/2
		}
		for i, ch := range label {
			if p := pos + i; p >= 0 && p < width && header[p] == ' ' {
				header[p] = ch
			}
		}
	}
	return string(header)
}

// ===== SNMP helpers =====

func oidIndex(oid, base string) (int, bool) {
	prefix := base + "."
	if !strings.HasPrefix(oid, prefix) {
		prefix = "." + base + "."
		if !strings.HasPrefix(oid, prefix) {
			return 0, false
		}
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

func counterDelta(curr, prev uint64, is64bit bool) float64 {
	if curr >= prev {
		return float64(curr - prev)
	}
	if is64bit {
		return float64(^uint64(0)-prev+curr) + 1
	}
	return float64(uint64(^uint32(0))-prev+curr) + 1
}

// bulkWalkMulti fetches N OID columns in parallel using a single GETBULK walk.
// Instead of N sequential BulkWalkAll calls, each round-trip retrieves one row
// across ALL columns simultaneously — roughly N× fewer UDP packets.
// Returns map[baseOID]map[ifIndex]value.  Stops when any column leaves its subtree.
func bulkWalkMulti(conn *gosnmp.GoSNMP, baseOIDs []string, maxRep uint32) (map[string]map[int]uint64, error) {
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

// ===== Per-switch polling goroutine =====

func pollSwitch(sw *SwitchData, delay float64, snmpTimeout time.Duration, sem chan struct{}, slowMs int64) {
	portNames := map[int]string{}
	portTypes := map[int]int{} // ifType per index
	prevStatus := ""
	delayDur := time.Duration(delay * float64(time.Second))

	newConn := func() *gosnmp.GoSNMP {
		return &gosnmp.GoSNMP{
			Target:         sw.IP,
			Port:           161,
			Community:      snmpCommunity,
			Version:        gosnmp.Version2c,
			Timeout:        snmpTimeout,
			Retries:        1,
			MaxRepetitions: 20, // used by BulkWalkAll for one-off initial walks
		}
	}

	for {
		start := time.Now()
		sem <- struct{}{}

		conn := newConn()
		if err := conn.Connect(); err != nil {
			<-sem
			logger.Printf("CONNECT_ERR %s (%s): %v", sw.IP, sw.Name, err)
			sw.mu.Lock()
			sw.ErrorCount++
			sw.Status = "TIMEOUT"
			sw.mu.Unlock()
			time.Sleep(delayDur)
			continue
		}

		// One-time: fetch interface names and types
		if len(portNames) == 0 {
			if pdus, err := conn.BulkWalkAll(OID_IFNAME); err != nil {
				logger.Printf("WALK_ERR %s (%s) ifName: %v", sw.IP, sw.Name, err)
			} else {
				for _, pdu := range pdus {
					if idx, ok := oidIndex(pdu.Name, OID_IFNAME); ok {
						switch v := pdu.Value.(type) {
						case string:
							portNames[idx] = v
						case []byte:
							portNames[idx] = string(v)
						}
					}
				}
			}
			if pdus, err := conn.BulkWalkAll(OID_IFTYPE); err != nil {
				logger.Printf("WALK_ERR %s (%s) ifType: %v", sw.IP, sw.Name, err)
			} else {
				for _, pdu := range pdus {
					if idx, ok := oidIndex(pdu.Name, OID_IFTYPE); ok {
						portTypes[idx] = int(snmpUint64(pdu))
					}
				}
			}
			logger.Printf("NAMES %s (%s): %d ifaces, %d phys",
				sw.IP, sw.Name, len(portNames), countPhys(portTypes))
		}

		// Main poll: all metric OIDs in a single multi-column GETBULK walk.
		// ts is recorded AFTER the walk so the sample timestamp matches when
		// the counter values were actually read — keeping it at the right edge.
		tables, _ := bulkWalkMulti(conn, metricOIDs, 20)
		ts := time.Now()
		conn.Conn.Close()
		<-sem

		pollMs := time.Since(start).Milliseconds()
		if slowMs > 0 && pollMs > slowMs {
			logger.Printf("SLOW %s (%s): %dms", sw.IP, sw.Name, pollMs)
		}

		tsF := float64(ts.UnixNano()) / 1e9
		cutoff := tsF - MAX_HIST_SEC

		sw.mu.Lock()
		sw.LastPollMs = pollMs
		sw.TotalPollMs += pollMs
		if len(tables[OID_HCIN]) == 0 {
			sw.ErrorCount++
			sw.Status = "TIMEOUT"
			if prevStatus != "TIMEOUT" {
				logger.Printf("TIMEOUT %s (%s) after %dms", sw.IP, sw.Name, pollMs)
			}
		} else {
			sw.PollCount++
			sw.IfaceCount = len(portNames)
			sw.PhysPorts = countPhys(portTypes)
			sw.Status = "OK"

			// Track actual inter-sample interval (EMA) for sparkline persistence
			if !sw.prevTS.IsZero() {
				dt := ts.Sub(sw.prevTS).Seconds()
				if sw.SampleInterval == 0 {
					sw.SampleInterval = dt
				} else {
					sw.SampleInterval = 0.7*sw.SampleInterval + 0.3*dt
				}
			}

			if prevStatus == "TIMEOUT" || prevStatus == "" {
				logger.Printf("OK %s (%s): %d phys/%d ifaces, %dms",
					sw.IP, sw.Name, sw.PhysPorts, sw.IfaceCount, pollMs)
			}

			dt := ts.Sub(sw.prevTS).Seconds()
			rates := make(map[string]*PortRate)
			var totalIn, totalOut float64

			for idx, currIn := range tables[OID_HCIN] {
				// Skip non-physical interfaces from rate calculation
				if t, known := portTypes[idx]; known && !physIfTypes[t] {
					continue
				}
				name, ok := portNames[idx]
				if !ok {
					name = fmt.Sprintf("if%d", idx)
				}
				rate := &PortRate{}
				if dt > 0 && !sw.prevTS.IsZero() {
					if prev, ok := sw.prevCounters[name]; ok {
						rate.In = counterDelta(currIn, prev[0], true) / dt / 1024
						rate.Out = counterDelta(tables[OID_HCOUT][idx], prev[1], true) / dt / 1024
						totalIn += rate.In
						totalOut += rate.Out
					}
				}
				if sw.prevCounters == nil {
					sw.prevCounters = make(map[string][2]uint64)
				}
				sw.prevCounters[name] = [2]uint64{currIn, tables[OID_HCOUT][idx]}
				rates[name] = rate
			}
			sw.prevTS = ts
			sw.In, sw.Out = totalIn, totalOut
			sw.HistIn = append(sw.HistIn, Sample{tsF, totalIn})
			sw.HistOut = append(sw.HistOut, Sample{tsF, totalOut})
			for len(sw.HistIn) > 0 && sw.HistIn[0].TS < cutoff {
				sw.HistIn = sw.HistIn[1:]
				sw.HistOut = sw.HistOut[1:]
			}
			if sw.PortHist == nil {
				sw.PortHist = make(map[string]*PortHistory)
			}
			for pname, r := range rates {
				if _, ok := sw.PortHist[pname]; !ok {
					sw.PortHist[pname] = &PortHistory{}
				}
				ph := sw.PortHist[pname]
				ph.In = append(ph.In, Sample{tsF, r.In})
				ph.Out = append(ph.Out, Sample{tsF, r.Out})
				for len(ph.In) > 0 && ph.In[0].TS < cutoff {
					ph.In = ph.In[1:]
					ph.Out = ph.Out[1:]
				}
			}
			sw.Rates = rates
		}
		prevStatus = sw.Status
		sw.mu.Unlock()

		if sleep := delayDur - time.Since(start); sleep > 50*time.Millisecond {
			time.Sleep(sleep)
		}
	}
}

func countPhys(portTypes map[int]int) int {
	n := 0
	for _, t := range portTypes {
		if physIfTypes[t] {
			n++
		}
	}
	return n
}

// ===== State persistence =====
//
// Binary format (STATE_FILE_BIN):
//   magic[8]  NSwitches:u32
//   per switch: IPLen:u8  IP  NHist:u32  HistIn-raw  HistOut-raw  NPorts:u32
//   per port:   NameLen:u8  Name  NIn:u32  In-raw  NOut:u32  Out-raw
//
// "raw" means len*16 bytes — packed IEEE-754 little-endian float64 pairs {TS,Val}.
// Zero reflection, zero extra allocation on load path (unsafe.Slice reinterpret).

var stateMagic = [8]byte{'M', 'I', 'F', 'S', 'T', 'A', 'T', 3}

// samplesToBytes reinterprets a []Sample as []byte with no copy.
// Safe: Sample is {TS, Val float64} — no padding, fixed size 16 bytes.
func samplesToBytes(s []Sample) []byte {
	if len(s) == 0 {
		return nil
	}
	sz := int(unsafe.Sizeof(s[0]))
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), sz*len(s))
}

// bytesToSamples allocates a fresh []Sample and bulk-copies raw bytes into it.
func bytesToSamples(b []byte) []Sample {
	sz := int(unsafe.Sizeof(Sample{}))
	n := len(b) / sz
	if n == 0 {
		return nil
	}
	s := make([]Sample, n)
	copy(unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(b)), b)
	return s
}

func pu32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func pu16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }

func writeU32(w *bufio.Writer, v uint32) {
	var b [4]byte; binary.LittleEndian.PutUint32(b[:], v); w.Write(b[:])
}
func writeStr(w *bufio.Writer, s string) {
	if len(s) > 255 { s = s[:255] }
	w.WriteByte(byte(len(s))); w.WriteString(s)
}
func writeSamples(w *bufio.Writer, s []Sample) {
	writeU32(w, uint32(len(s)))
	if len(s) > 0 { w.Write(samplesToBytes(s)) }
}

func readFull(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n); _, err := io.ReadFull(r, b); return b, err
}
func readU32(r io.Reader) (uint32, error) {
	b, err := readFull(r, 4); return pu32(b), err
}
func readStr(r io.Reader) (string, error) {
	b1 := []byte{0}
	if _, err := io.ReadFull(r, b1); err != nil { return "", err }
	if b1[0] == 0 { return "", nil }
	b, err := readFull(r, int(b1[0])); return string(b), err
}
func readSamples(r io.Reader) ([]Sample, error) {
	n, err := readU32(r)
	if err != nil || n == 0 { return nil, err }
	sz := int(unsafe.Sizeof(Sample{}))
	b, err := readFull(r, int(n)*sz)
	if err != nil { return nil, err }
	return bytesToSamples(b), nil
}

func saveState(states []*SwitchData) {
	f, err := os.Create(STATE_FILE_BIN)
	if err != nil {
		return
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)

	w.Write(stateMagic[:])

	// Count switches that actually have history.
	var sws []*SwitchData
	for _, sw := range states {
		sw.mu.RLock()
		if len(sw.HistIn) > 0 { sws = append(sws, sw) }
		sw.mu.RUnlock()
	}
	writeU32(w, uint32(len(sws)))

	for _, sw := range sws {
		sw.mu.RLock()
		writeStr(w, sw.IP)
		writeSamples(w, sw.HistIn)
		writeSamples(w, sw.HistOut)
		// Count ports with data.
		nport := 0
		for _, ph := range sw.PortHist {
			if len(ph.In) > 0 { nport++ }
		}
		writeU32(w, uint32(nport))
		for pname, ph := range sw.PortHist {
			if len(ph.In) == 0 { continue }
			writeStr(w, pname)
			writeSamples(w, ph.In)
			writeSamples(w, ph.Out)
		}
		sw.mu.RUnlock()
	}
	w.Flush()
}

func loadStateBin(path string) (SaveState, error) {
	var s SaveState
	f, err := os.Open(path)
	if err != nil { return s, err }
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<20)
	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil { return s, err }
	if magic != stateMagic { return s, fmt.Errorf("bad magic") }

	nSw, err := readU32(r)
	if err != nil { return s, err }

	s.HistIn   = make(map[string][]Sample, nSw)
	s.HistOut  = make(map[string][]Sample, nSw)
	s.PortHist = make(map[string]map[string]*PortHistory, nSw)

	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r); if err != nil { return s, err }
		histIn,  err := readSamples(r); if err != nil { return s, err }
		histOut, err := readSamples(r); if err != nil { return s, err }
		s.HistIn[ip]  = histIn
		s.HistOut[ip] = histOut
		nPort, err := readU32(r); if err != nil { return s, err }
		ph := make(map[string]*PortHistory, nPort)
		for j := uint32(0); j < nPort; j++ {
			pname, err := readStr(r); if err != nil { return s, err }
			pIn,   err := readSamples(r); if err != nil { return s, err }
			pOut,  err := readSamples(r); if err != nil { return s, err }
			ph[pname] = &PortHistory{In: pIn, Out: pOut}
		}
		s.PortHist[ip] = ph
	}
	return s, nil
}

func loadState() SaveState {
	s, _ := loadStateBin(STATE_FILE_BIN)
	return s
}

func getSwitches() []map[string]string {
	var result []map[string]string
	f, err := os.Open("/dev/shm/sw-ip-name-mac")
	if err != nil {
		return result
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) >= 2 {
			result = append(result, map[string]string{"ip": parts[0], "name": parts[1]})
		}
	}
	return result
}

// ===== Benchmark mode =====

// runBenchmark polls all switches once concurrently and prints a timing table.
// It bypasses the TUI entirely — useful for diagnosing slow/broken switches.
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
		if l := len(r.IP); l > bwIP { bwIP = l }
		if l := len(r.Name); l > bwName { bwName = l }
		if l := len(r.Status); l > bwStatus { bwStatus = l }
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
		totalSem  += r.SemWaitMs
		if r.SnmpMs > maxSnmp { maxSnmp = r.SnmpMs }
		if r.SemWaitMs > maxSem { maxSem = r.SemWaitMs }
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

// ===== TUI =====

type DisplayItem struct {
	IP, Name, SwName, Port, Status string
	In, Out                        float64
	Hist                           []Sample
	SampleInterval                 float64
	Detail                         bool
}

func main() {
	delay       := flag.Float64("d", 1.0, "poll interval in seconds (e.g. 0.5, 1, 2)")
	snmpTimeout := flag.Duration("snmptimeout", 3*time.Second, "SNMP timeout per poll (reduce for sub-second delay)")
	logPath := flag.String("log", "", "log SNMP errors and perf to file (e.g. /tmp/mifstat.log)")
	bench  := flag.Bool("bench", false, "benchmark all switches once and exit (no TUI)")
	slowMs := flag.Int64("slowms", 500, "log polls slower than this (ms); 0=disable")
	flag.Parse()
	targets := flag.Args()

	if *logPath != "" {
		closeLog := initLogger(*logPath)
		defer closeLog()
	}

	allSwitches := getSwitches()
	var switches []map[string]string
	if len(targets) == 0 {
		switches = allSwitches
	} else {
		for _, sw := range allSwitches {
			for _, t := range targets {
				if sw["name"] == t || sw["ip"] == t {
					switches = append(switches, sw)
					break
				}
			}
		}
	}
	if len(switches) == 0 {
		fmt.Fprintln(os.Stderr, "No switches found.")
		os.Exit(1)
	}

	sem := make(chan struct{}, 50) // cap concurrent SNMP sessions

	// -bench: one-shot timing table, no TUI
	if *bench {
		runBenchmark(switches, sem)
		return
	}

	saved := loadState()
	states := make([]*SwitchData, len(switches))
	for i, sw := range switches {
		sd := &SwitchData{
			Name:           sw["name"],
			IP:             sw["ip"],
			Status:         "WAITING",
			Rates:          make(map[string]*PortRate),
			PortHist:       make(map[string]*PortHistory),
			SampleInterval: *delay, // conservative default until first real measurement
		}
		if saved.HistIn != nil {
			sd.HistIn = saved.HistIn[sw["ip"]]
			sd.HistOut = saved.HistOut[sw["ip"]]
			if ph := saved.PortHist[sw["ip"]]; ph != nil {
				sd.PortHist = ph
			}
		}
		states[i] = sd
		go pollSwitch(sd, *delay, *snmpTimeout, sem, *slowMs)
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		panic(err)
	}
	if err = screen.Init(); err != nil {
		panic(err)
	}
	defer screen.Fini()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		saveState(states)
		screen.Fini()
		os.Exit(0)
	}()

	eventCh := make(chan tcell.Event, 10)
	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				return
			}
			eventCh <- ev
		}
	}()

	showDetail := false
	showPerf   := false
	sortKey := "out"
	autoSort := true
	zoomIdx := 0
	var viewNow *float64
	var prevItems []DisplayItem
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case ev := <-eventCh:
			switch e := ev.(type) {
			case *tcell.EventKey:
				zoom := zoomLevels[zoomIdx]
				switch {
				case e.Rune() == 'q':
					saveState(states)
					return
				case e.Rune() == 'p':
					showPerf = !showPerf
				case e.Rune() == 'd':
					showDetail = !showDetail
					showPerf = false
					autoSort = true
					prevItems = nil
				case e.Rune() == ' ':
					autoSort = !autoSort
				case e.Rune() == '1':
					sortKey = "ip"
				case e.Rune() == '2':
					sortKey = "name"
				case e.Rune() == 'i':
					sortKey = "in"
				case e.Rune() == 'o':
					sortKey = "out"
				case e.Rune() == '+':
					if zoomIdx > 0 {
						zoomIdx--
					}
				case e.Rune() == '-':
					if zoomIdx < len(zoomLevels)-1 {
						zoomIdx++
					}
				case e.Key() == tcell.KeyLeft:
					if viewNow == nil {
						t := float64(time.Now().UnixNano()) / 1e9
						viewNow = &t
					}
					*viewNow -= *delay * float64(zoom)
				case e.Key() == tcell.KeyRight:
					if viewNow != nil {
						*viewNow += *delay * float64(zoom)
						if *viewNow >= float64(time.Now().UnixNano())/1e9 {
							viewNow = nil
						}
					}
				case e.Key() == tcell.KeyPgUp:
					_, w := screen.Size()
					if viewNow == nil {
						t := float64(time.Now().UnixNano()) / 1e9
						viewNow = &t
					}
					*viewNow -= float64(w) * *delay * float64(zoom)
				case e.Key() == tcell.KeyPgDn:
					if viewNow != nil {
						_, w := screen.Size()
						*viewNow += float64(w) * *delay * float64(zoom)
						if *viewNow >= float64(time.Now().UnixNano())/1e9 {
							viewNow = nil
						}
					}
				case e.Key() == tcell.KeyEnter:
					viewNow = nil
				}
			case *tcell.EventResize:
				screen.Sync()
			}
		case <-ticker.C:
		}

		now := float64(time.Now().UnixNano()) / 1e9
		zoom := zoomLevels[zoomIdx]
		pixelSec := *delay * float64(zoom)
		qNow := math.Floor(now/pixelSec) * pixelSec
		dispNow := qNow
		if viewNow != nil {
			dispNow = *viewNow
		}

		var items []DisplayItem
		for _, sw := range states {
			sw.mu.RLock()
			si := sw.SampleInterval
			if !showDetail {
				hist := sw.HistOut
				if sortKey == "in" {
					hist = sw.HistIn
				}
				items = append(items, DisplayItem{
					IP: sw.IP, Name: sw.Name, Status: sw.Status,
					In: sw.In, Out: sw.Out, Hist: hist, SampleInterval: si,
				})
			} else {
				for pname, r := range sw.Rates {
					if r.In > 0.1 || r.Out > 0.1 {
						var hist []Sample
						if ph, ok := sw.PortHist[pname]; ok {
							if sortKey == "in" {
								hist = ph.In
							} else {
								hist = ph.Out
							}
						}
						items = append(items, DisplayItem{
							IP: sw.IP, SwName: sw.Name, Port: pname, Status: sw.Status,
							In: r.In, Out: r.Out,
							Hist: hist, SampleInterval: si, Detail: true,
						})
					}
				}
			}
			sw.mu.RUnlock()
		}

		if autoSort {
			sort.Slice(items, func(i, j int) bool {
				switch sortKey {
				case "in":
					return items[i].In > items[j].In
				case "ip":
					return items[i].IP < items[j].IP
				case "name":
					n := items[i].Name
					if items[i].Detail {
						n = items[i].SwName
					}
					m := items[j].Name
					if items[j].Detail {
						m = items[j].SwName
					}
					return n < m
				}
				return items[i].Out > items[j].Out
			})
		} else if len(prevItems) > 0 {
			// Freeze: reorder fresh data to match previous ordering (like original mifstat).
			// Build lookup by IP+Port key, then walk prevItems to restore order.
			type diKey struct{ ip, port string }
			byKey := make(map[diKey]DisplayItem, len(items))
			for _, it := range items {
				byKey[diKey{it.IP, it.Port}] = it
			}
			reordered := make([]DisplayItem, 0, len(items))
			seen := make(map[diKey]bool, len(items))
			for _, prev := range prevItems {
				k := diKey{prev.IP, prev.Port}
				if updated, ok := byKey[k]; ok {
					reordered = append(reordered, updated)
					seen[k] = true
				}
			}
			// Append any new items not present in previous frame at the bottom.
			for _, it := range items {
				if !seen[diKey{it.IP, it.Port}] {
					reordered = append(reordered, it)
				}
			}
			items = reordered
		}
		prevItems = items

		screen.Clear()
		w, h := screen.Size()
		defStyle := tcell.StyleDefault
		revStyle := defStyle.Reverse(true)
		dimStyle := defStyle.Dim(true)
		warnStyle := defStyle.Foreground(tcell.ColorYellow)

		// ---- Perf view ('p') ----
		if showPerf {
			type perfRow struct {
				IP, Name, Status string
				Polls, Errors    uint64
				PhysPorts        int
				IfaceCount       int
				LastMs, AvgMs    int64
				SampleSec        float64
			}
			rows := make([]perfRow, len(states))
			for i, sw := range states {
				sw.mu.RLock()
				avg := int64(0)
				if sw.PollCount > 0 {
					avg = sw.TotalPollMs / int64(sw.PollCount)
				}
				rows[i] = perfRow{
					IP: sw.IP, Name: sw.Name, Status: sw.Status,
					Polls: sw.PollCount, Errors: sw.ErrorCount,
					PhysPorts: sw.PhysPorts, IfaceCount: sw.IfaceCount,
					LastMs: sw.LastPollMs, AvgMs: avg,
					SampleSec: sw.SampleInterval,
				}
				sw.mu.RUnlock()
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].AvgMs > rows[j].AvgMs })

			pwIP, pwName, pwStatus := len("IP"), len("Name"), len("Status")
			for _, r := range rows {
				if l := len(r.IP); l > pwIP { pwIP = l }
				if l := len(r.Name); l > pwName { pwName = l }
				if l := len(r.Status); l > pwStatus { pwStatus = l }
			}
			hdr := fmt.Sprintf("%-*s %-*s %-*s %6s %6s %5s %6s %8s %8s %8s",
				pwIP, "IP", pwName, "Name", pwStatus, "Status",
				"Polls", "Errors", "Phys", "Ifaces", "Last(ms)", "Avg(ms)", "Rate(s)")
			drawStr(screen, 0, 0, hdr[:min(len(hdr), w-1)], revStyle)
			for i, r := range rows {
				if i >= h-2 {
					break
				}
				st := defStyle
				if r.Errors > 0 || r.Status != "OK" {
					st = warnStyle
				}
				line := fmt.Sprintf("%-*s %-*s %-*s %6d %6d %5d %6d %8d %8d %8.1f",
					pwIP, r.IP, pwName, r.Name, pwStatus, r.Status,
					r.Polls, r.Errors, r.PhysPorts, r.IfaceCount, r.LastMs, r.AvgMs, r.SampleSec)
				drawStr(screen, 0, i+1, line[:min(len(line), w-1)], st)
			}
			statusLine := "p:hide-perf  q:quit  (sorted by avg poll time; Phys=ethernetCsmacd+LAG, Ifaces=all SNMP)"
			drawStr(screen, 0, h-1, statusLine[:min(len(statusLine), w-1)], dimStyle)
			screen.Show()
			continue
		}

		// ---- Normal / detail view ----
		// Compute dynamic column widths from actual content.
		wIP, wName := len("IP"), len("Name")
		wSw, wPort := len("Switch"), len("Port")
		for _, item := range items {
			pfx := ""
			if item.Status != "OK" && item.Status != "WAITING" {
				pfx = fmt.Sprintf("[%s] ", item.Status)
			}
			if !item.Detail {
				if l := len(item.IP); l > wIP { wIP = l }
				if l := len(pfx + item.Name); l > wName { wName = l }
			} else {
				if l := len(pfx + item.SwName); l > wSw { wSw = l }
				if l := len(item.Port); l > wPort { wPort = l }
			}
		}

		var hdr string
		if !showDetail {
			hdr = fmt.Sprintf("%-*s %-*s %12s %12s | ", wIP, "IP", wName, "Name", "IN(KB/s)", "OUT(KB/s)")
		} else {
			hdr = fmt.Sprintf("%-*s %-*s %10s %10s | ", wSw, "Switch", wPort, "Port", "IN(KB/s)", "OUT(KB/s)")
		}
		sparkW := w - len(hdr) - 1
		trendHdr := getTrendHeader(sparkW, *delay, zoom, dispNow)
		drawStr(screen, 0, 0, (hdr+trendHdr)[:min(len(hdr+trendHdr), w-1)], revStyle)

		for i, item := range items {
			if i >= h-2 {
				break
			}
			statusPfx := ""
			if item.Status != "OK" && item.Status != "WAITING" {
				statusPfx = fmt.Sprintf("[%s] ", item.Status)
			}
			var line string
			if !item.Detail {
				line = fmt.Sprintf("%-*s %-*s %12.2f %12.2f | ",
					wIP, item.IP, wName, statusPfx+item.Name, item.In, item.Out)
			} else {
				line = fmt.Sprintf("%-*s %-*s %10.2f %10.2f | ",
					wSw, statusPfx+item.SwName, wPort, item.Port, item.In, item.Out)
			}
			sparkChars, sparkStale := getSparkline(item.Hist, sparkW, *delay, zoom, dispNow, item.SampleInterval)
			lineRunes := []rune(line)
			for k, ch := range lineRunes {
				if k >= w {
					break
				}
				screen.SetContent(k, i+1, ch, nil, defStyle)
			}
			xSpark := len(lineRunes)
			for k, ch := range sparkChars {
				if xSpark+k >= w {
					break
				}
				st := defStyle
				if sparkStale[k] {
					st = dimStyle
				}
				screen.SetContent(xSpark+k, i+1, ch, nil, st)
			}
		}

		scroll := ""
		if viewNow != nil {
			scroll = "[PAST] "
		}
		frozen := "[AUTO]"
		if !autoSort {
			frozen = "[FROZEN]"
		}
		delayStr := fmt.Sprintf("%.4g", *delay)
		statusLine := fmt.Sprintf("%s %sd=%ss zoom:1/%dx  q:quit d:detail p:perf i/o:sort ARROWS:scroll ENTER:now",
			frozen, scroll, delayStr, zoom)
		drawStr(screen, 0, h-1, statusLine[:min(len(statusLine), w-1)], dimStyle)
		nowStr := time.Now().Format("2006-01-02 15:04:05")
		if w-len(nowStr)-1 > len(statusLine)+2 {
			drawStr(screen, w-len(nowStr)-1, h-1, nowStr, dimStyle)
		}
		screen.Show()
	}
}

func drawStr(screen tcell.Screen, x, y int, s string, style tcell.Style) {
	for i, ch := range []rune(s) {
		screen.SetContent(x+i, y, ch, nil, style)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
