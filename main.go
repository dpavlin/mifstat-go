package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
)

const version = "0.3.0"

var (
	snmpCommunity string
	logger        = log.New(io.Discard, "", 0)
)

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

func getCommunity(flagCommunity string) string {
	if flagCommunity != "" {
		return flagCommunity
	}
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

func getSwitches(path string) []map[string]string {
	var result []map[string]string
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open switch file %s: %v\n", path, err)
		return result
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			result = append(result, map[string]string{"ip": parts[0], "name": parts[1]})
		}
	}
	return result
}

func matchesFilter(swName, swIP, filterLower string) bool {
	if filterLower == "" {
		return true
	}
	return strings.Contains(strings.ToLower(swName), filterLower) || strings.Contains(strings.ToLower(swIP), filterLower)
}

func getSlowMs(val int64, delay float64, isSet bool) int64 {
	if isSet {
		return val
	}
	return int64(delay * 1000)
}

func main() {
	delay := flag.Float64("d", 1.0, "poll interval in seconds (e.g. 0.5, 1, 2)")
	snmpTimeout := flag.Duration("snmptimeout", 3*time.Second, "SNMP timeout per poll (reduce for sub-second delay)")
	logPath := flag.String("log", "", "log SNMP errors and perf to file (e.g. /tmp/mifstat.log)")
	bench := flag.Bool("bench", false, "benchmark all switches once and exit (no TUI)")
	slowMsFlag := flag.Int64("slowms", 0, "log polls slower than this (ms); defaults to -d * 1000")
	community := flag.String("c", "", "SNMP community string (overrides ~/.config/snmp.community)")
	swFile := flag.String("f", "/dev/shm/sw-ip-name-mac", "switch list file (IP NAME [MAC])")
	stateFile := flag.String("state", "/tmp/mifstat_go.bin", "state file to save history")
	vFlag := flag.Bool("version", false, "show version and exit")
	histHours := flag.Float64("hist", 6.0, "history duration in hours")
	flag.Parse()

	MAX_HIST_SEC = (*histHours) * 3600.0

	slowMsSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "slowms" {
			slowMsSet = true
		}
	})
	slowMs := getSlowMs(*slowMsFlag, *delay, slowMsSet)

	if *vFlag {
		fmt.Printf("mifstat version %s\n", version)
		os.Exit(0)
	}
	targets := flag.Args()

	snmpCommunity = getCommunity(*community)

	if *logPath != "" {
		closeLog := initLogger(*logPath)
		defer closeLog()
	}

	allSwitches := getSwitches(*swFile)
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

	sem := make(chan struct{}, 50)

	if *bench {
		runBenchmark(switches, sem, slowMs)
		return
	}

	saved := loadState(*stateFile)
	states := make([]*SwitchData, len(switches))
	maxSamples := int(MAX_HIST_SEC / *delay) + 1

	for i, sw := range switches {
		sd := &SwitchData{
			Name:           sw["name"],
			IP:             sw["ip"],
			Status:         "WAITING",
			Rates:          make(map[string]*PortRate),
			PortHist:       make(map[string]*PortHistory),
			SampleInterval: *delay,
			MaxRepetitions: 20,
			Timestamps:     NewFloat64Ring(maxSamples),
			HistIn:         NewFloat32Ring(maxSamples),
			HistOut:        NewFloat32Ring(maxSamples),
			LatHist:        NewFloat32Ring(maxSamples),
		}
		if saved.HistIn != nil {
			ip := sw["ip"]
			tsList := saved.Timestamps[ip]
			inList := saved.HistIn[ip]
			outList := saved.HistOut[ip]
			latList := saved.LatHist[ip]
			
			// Migration/Fallback: if V1, we might not have unified Timestamps map.
			// But loadState already handles V1 -> SaveState conversion.
			for j, t := range tsList {
				sd.Timestamps.Push(t)
				if j < len(inList) { sd.HistIn.Push(inList[j]) }
				if j < len(outList) { sd.HistOut.Push(outList[j]) }
				if j < len(latList) { sd.LatHist.Push(latList[j]) }
			}

			if phMap := saved.PortHist[ip]; phMap != nil {
				for pname, phData := range phMap {
					ph := &PortHistory{
						In:  NewFloat32Ring(maxSamples),
						Out: NewFloat32Ring(maxSamples),
					}
					// Align port history with timestamps if possible, 
					// but SaveState already has them as flat slices.
					for _, v := range phData.In {
						ph.In.Push(v)
					}
					for _, v := range phData.Out {
						ph.Out.Push(v)
					}
					sd.PortHist[pname] = ph
				}
			}
		}
		states[i] = sd
		go pollSwitch(sd, *delay, *snmpTimeout, sem, slowMs)
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
		saveState(states, *stateFile)
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
	showPerf := false
	showTraffic := false
	sortKey := "out"
	autoSort := map[string]bool{"main": true, "detail": true, "perf": true, "traffic": true}
	viewMode := 0 // 0: Sparkline, 1: Numeric
	zoomIdx := 0
	var viewNow *float64
	var prevItems []DisplayItem
	ticker := time.NewTicker(50 * time.Millisecond)
	saveTicker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	defer saveTicker.Stop()

	filtering := false
	filterStr := ""

	for {
		// Determine which screen's sort state to use.
		currScreen := "main"
		if showPerf {
			currScreen = "perf"
		} else if showTraffic {
			currScreen = "traffic"
		} else if showDetail {
			currScreen = "detail"
		}

		select {
		case <-saveTicker.C:
			saveState(states, *stateFile)
		case ev := <-eventCh:
			switch e := ev.(type) {
			case *tcell.EventKey:
				if filtering {
					if e.Key() == tcell.KeyEsc || e.Key() == tcell.KeyEnter {
						filtering = false
						if e.Key() == tcell.KeyEsc {
							filterStr = ""
						}
					} else if e.Key() == tcell.KeyBackspace || e.Key() == tcell.KeyBackspace2 {
						if len(filterStr) > 0 {
							filterStr = filterStr[:len(filterStr)-1]
						}
					} else if e.Rune() != 0 {
						filterStr += string(e.Rune())
					}
					continue
				}

				zoom := zoomLevels[zoomIdx]
				switch {
				case e.Rune() == 'q':
					saveState(states, *stateFile)
					return
				case e.Rune() == '/':
					filtering = true
				case e.Rune() == 'p':
					showPerf = !showPerf
					showTraffic = false
				case e.Rune() == 't':
					showTraffic = !showTraffic
					showPerf = false
				case e.Rune() == 'd':
					showDetail = !showDetail
					showPerf = false
					showTraffic = false
					prevItems = nil
				case e.Rune() == ' ':
					autoSort[currScreen] = !autoSort[currScreen]
				case e.Rune() == 'v':
					viewMode = (viewMode + 1) % 2
				case e.Rune() == '1', e.Rune() == 'a':
					sortKey = "ip"
				case e.Rune() == '2', e.Rune() == 'n':
					sortKey = "name"
				case e.Rune() == '3', e.Rune() == 's':
					sortKey = "status"
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
		filterLower := strings.ToLower(filterStr)
		for _, sw := range states {
			sw.mu.RLock()
			si := sw.SampleInterval
			if !showDetail {
				if !matchesFilter(sw.Name, sw.IP, filterLower) {
					sw.mu.RUnlock()
					continue
				}
				items = append(items, DisplayItem{
					IP: sw.IP, Name: sw.Name, Status: sw.Status,
					In: sw.In, Out: sw.Out,
					EmaIn: sw.EmaIn, EmaOut: sw.EmaOut,
					MaxIn: sw.MaxIn, MaxOut: sw.MaxOut,
					TimestampsRing: &sw.Timestamps,
					HistRing:       &sw.HistOut,
					LatHistRing:    &sw.LatHist,
					SwSampleInterval: si,
					SwLastPollMs:     sw.LastPollMs,
					SlowMs:           slowMs,
				})
				if sortKey == "in" {
					items[len(items)-1].HistRing = &sw.HistIn
				}
			} else {
				if !matchesFilter(sw.Name, sw.IP, filterLower) {
					sw.mu.RUnlock()
					continue
				}
				for pname, r := range sw.Rates {
					if r.In > 0.1 || r.Out > 0.1 {
						var histRing *Float32Ring
						if ph, ok := sw.PortHist[pname]; ok {
							histRing = &ph.Out
							if sortKey == "in" {
								histRing = &ph.In
							}
						}
						items = append(items, DisplayItem{
							IP: sw.IP, SwName: sw.Name, Port: pname, Status: sw.Status,
							In: r.In, Out: r.Out,
							EmaIn: r.EmaIn, EmaOut: r.EmaOut,
							MaxIn: r.MaxIn, MaxOut: r.MaxOut,
							TimestampsRing: &sw.Timestamps,
							HistRing:       histRing,
							LatHistRing:    &sw.LatHist,
							SwSampleInterval: si,
							SwLastPollMs:     sw.LastPollMs,
							SlowMs:           slowMs,
							Detail:         true,
						})
					}
				}
			}
			sw.mu.RUnlock()
		}

		if autoSort[currScreen] {
			sort.Slice(items, func(i, j int) bool {
				switch sortKey {
				case "in":
					return items[i].EmaIn > items[j].EmaIn
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
				case "status":
					if items[i].Status != items[j].Status {
						// Put OK at the end
						if items[i].Status == "OK" {
							return false
						}
						if items[j].Status == "OK" {
							return true
						}
						return items[i].Status < items[j].Status
					}
					return items[i].EmaOut > items[j].EmaOut
				}
				return items[i].EmaOut > items[j].EmaOut
			})
		} else if len(prevItems) > 0 {
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

		if showPerf {
			renderPerf(screen, states, h, w, revStyle, warnStyle, defStyle, dimStyle, autoSort["perf"])
			continue
		}
		if showTraffic {
			renderTraffic(screen, items, h, w, revStyle, warnStyle, defStyle, dimStyle, autoSort["traffic"])
			continue
		}

		renderMain(screen, items, h, w, delay, zoom, dispNow, revStyle, defStyle, dimStyle, autoSort[currScreen], viewNow, viewMode, sortKey, filtering, filterStr, slowMs)
		screen.Show()
	}
}

func renderTraffic(screen tcell.Screen, items []DisplayItem, h, w int, revStyle, warnStyle, defStyle, dimStyle tcell.Style, autoSort bool) {
	if len(items) == 0 {
		return
	}

	headers := []string{"IP", "Name", "IN", "OUT", "Avg IN", "Avg OUT", "Max IN", "Max OUT"}
	if items[0].Detail {
		headers[1] = "Port"
	}
	// Alignments: -1 for left, 1 for right
	aligns := []int{-1, -1, 1, 1, 1, 1, 1, 1}

	var rows [][]string
	for _, item := range items {
		name := item.Name
		if item.Detail {
			name = item.Port
		}
		rows = append(rows, []string{
			item.IP, name,
			formatRateCompact(item.In), formatRateCompact(item.Out),
			formatRateCompact(item.EmaIn), formatRateCompact(item.EmaOut),
			formatRateCompact(item.MaxIn), formatRateCompact(item.MaxOut),
		})
	}

	layout := NewTableLayout(headers, rows, aligns, 1)
	hdr := layout.FormatHeader(headers)
	drawStr(screen, 0, 0, hdr[:min(len(hdr), w-1)], revStyle)

	for i, item := range items {
		if i >= h-2 {
			break
		}
		st := defStyle
		if item.Status != "OK" && item.Status != "WAITING" {
			st = warnStyle
		}
		line := layout.FormatRow(rows[i])
		drawStr(screen, 0, i+1, line[:min(len(line), w-1)], st)
	}
	frozen := "[AUTO]"
	if !autoSort {
		frozen = "[FROZEN]"
	}
	statusLine := fmt.Sprintf("%s t:hide-traffic q:quit | (Numeric traffic summary; current, 1m avg, and session peak)", frozen)
	drawStr(screen, 0, h-1, statusLine[:min(len(statusLine), w-1)], dimStyle)
	screen.Show()
}

func renderPerf(screen tcell.Screen, states []*SwitchData, h, w int, revStyle, warnStyle, defStyle, dimStyle tcell.Style, autoSort bool) {
	type perfRow struct {
		IP, Name, Status string
		Polls, Errors    uint64
		PhysPorts        int
		IfaceCount       int
		LastMs, AvgMs    int64
		SampleSec        float64
		MaxRep           uint32
	}
	dataRows := make([]perfRow, len(states))
	for i, sw := range states {
		sw.mu.RLock()
		avg := int64(0)
		if sw.PollCount > 0 {
			avg = sw.TotalPollMs / int64(sw.PollCount)
		}
		dataRows[i] = perfRow{
			IP: sw.IP, Name: sw.Name, Status: sw.Status,
			Polls: sw.PollCount, Errors: sw.ErrorCount,
			PhysPorts: sw.PhysPorts, IfaceCount: sw.IfaceCount,
			LastMs: sw.LastPollMs, AvgMs: avg,
			SampleSec: sw.SampleInterval,
			MaxRep:    sw.MaxRepetitions,
		}
		sw.mu.RUnlock()
	}
	sort.Slice(dataRows, func(i, j int) bool { return dataRows[i].AvgMs > dataRows[j].AvgMs })

	headers := []string{"IP", "Name", "Status", "Polls", "Errors", "Phys", "Ifaces", "Last(ms)", "Avg(ms)", "Rate(s)", "MRep"}
	aligns := []int{-1, -1, -1, 1, 1, 1, 1, 1, 1, 1, 1}
	var rows [][]string
	for _, r := range dataRows {
		rows = append(rows, []string{
			r.IP, r.Name, r.Status,
			fmt.Sprintf("%d", r.Polls), fmt.Sprintf("%d", r.Errors),
			fmt.Sprintf("%d", r.PhysPorts), fmt.Sprintf("%d", r.IfaceCount),
			fmt.Sprintf("%d", r.LastMs), fmt.Sprintf("%d", r.AvgMs),
			fmt.Sprintf("%.1f", r.SampleSec), fmt.Sprintf("%d", r.MaxRep),
		})
	}

	layout := NewTableLayout(headers, rows, aligns, 1)
	hdr := layout.FormatHeader(headers)
	drawStr(screen, 0, 0, hdr[:min(len(hdr), w-1)], revStyle)

	for i, r := range dataRows {
		if i >= h-2 {
			break
		}
		st := defStyle
		if r.Errors > 0 || r.Status != "OK" {
			st = warnStyle
		}
		line := layout.FormatRow(rows[i])
		drawStr(screen, 0, i+1, line[:min(len(line), w-1)], st)
	}
	frozen := "[AUTO]"
	if !autoSort {
		frozen = "[FROZEN]"
	}
	statusLine := fmt.Sprintf("%s p:hide-perf q:quit | (Phys=ethernetCsmacd+LAG, Ifaces=all SNMP)", frozen)
	drawStr(screen, 0, h-1, statusLine[:min(len(statusLine), w-1)], dimStyle)
	screen.Show()
}

func renderMain(screen tcell.Screen, items []DisplayItem, h, w int, delay *float64, zoom int, dispNow float64, revStyle, defStyle, dimStyle tcell.Style, autoSort bool, viewNow *float64, viewMode int, sortKey string, filtering bool, filterStr string, slowMs int64) {
	wIP, wName := len("IP"), len("Name")
	wSw, wPort := len("Switch"), len("Port")
	for _, item := range items {
		pfx := ""
		if item.Status != "OK" && item.Status != "WAITING" {
			pfx = fmt.Sprintf("[%s] ", item.Status)
		}
		if !item.Detail {
			if l := len(item.IP); l > wIP {
				wIP = l
			}
			if l := len(pfx+item.Name); l > wName {
				wName = l
			}
		} else {
			if l := len(pfx+item.SwName); l > wSw {
				wSw = l
			}
			if l := len(item.Port); l > wPort {
				wPort = l
			}
		}
	}

	var hdr string
	showDetail := false
	if len(items) > 0 && items[0].Detail {
		showDetail = true
	}

	if !showDetail {
		hdr = fmt.Sprintf("%-*s %-*s %12s %12s | ", wIP, "IP", wName, "Name", "IN", "OUT")
	} else {
		hdr = fmt.Sprintf("%-*s %-*s %12s %12s | ", wSw, "Switch", wPort, "Port", "IN", "OUT")
	}
	sparkW := w - len(hdr) - 1

	if viewMode == 1 {
		hdr += getNumericHeader(sparkW, *delay, zoom)
		drawStr(screen, 0, 0, hdr[:min(len(hdr), w-1)], revStyle)
	} else {
		trendHdr := getTrendHeader(sparkW, *delay, zoom, dispNow)
		drawStr(screen, 0, 0, (hdr+trendHdr)[:min(len(hdr+trendHdr), w-1)], revStyle)
	}

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
			line = fmt.Sprintf("%-*s %-*s %12s %12s | ",
				wIP, item.IP, wName, statusPfx+item.Name, formatRate(item.In), formatRate(item.Out))
		} else {
			line = fmt.Sprintf("%-*s %-*s %12s %12s | ",
				wSw, statusPfx+item.SwName, wPort, item.Port, formatRate(item.In), formatRate(item.Out))
		}

		lineRunes := []rune(line)
		for k, ch := range lineRunes {
			if k >= w {
				break
			}
			screen.SetContent(k, i+1, ch, nil, defStyle)
		}
		xSpark := len(lineRunes)

		swSampleInterval := item.SwSampleInterval
		swLastPollMs := item.SwLastPollMs

		if viewMode == 1 {
			numStr := getNumericHistory(item.TimestampsRing, item.HistRing, dispNow, sparkW, *delay, zoom, swSampleInterval)
			drawStr(screen, xSpark, i+1, numStr, defStyle)
		} else {
			sparkChars, sparkStale := getSparkline(item.TimestampsRing, item.HistRing, item.LatHistRing, sparkW, *delay, zoom, dispNow, swSampleInterval, swLastPollMs, slowMs)
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
	}

	scroll := ""
	if viewNow != nil {
		scroll = "[PAST] "
	}
	frozen := "[AUTO]"
	if !autoSort {
		frozen = "[FROZEN]"
	}
	vModes := []string{"SPARK", "NUMER"}
	delayStr := fmt.Sprintf("%.4g", *delay)
	sortIndicator := fmt.Sprintf("sort:%s", sortKey)
	
	fStr := ""
	if filtering {
		fStr = fmt.Sprintf(" [FILTER: %s_]", filterStr)
	} else if filterStr != "" {
		fStr = fmt.Sprintf(" [filter: %s]", filterStr)
	}

	statusLine := fmt.Sprintf("%s %s %s %s d=%ss z=1/%d%s | q:quit d:det p:perf t:traf v:view /:filt i/o/n/a/s:sort +/-:zoom arrows:scroll SPC:auto",
		frozen, vModes[viewMode], sortIndicator, scroll, delayStr, zoom, fStr)
	drawStr(screen, 0, h-1, statusLine[:min(len(statusLine), w-1)], dimStyle)
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	if w-len(nowStr)-1 > len(statusLine)+2 {
		drawStr(screen, w-len(nowStr)-1, h-1, nowStr, dimStyle)
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
