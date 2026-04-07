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

const version = "0.1.0"

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

func main() {
	delay := flag.Float64("d", 1.0, "poll interval in seconds (e.g. 0.5, 1, 2)")
	snmpTimeout := flag.Duration("snmptimeout", 3*time.Second, "SNMP timeout per poll (reduce for sub-second delay)")
	logPath := flag.String("log", "", "log SNMP errors and perf to file (e.g. /tmp/mifstat.log)")
	bench := flag.Bool("bench", false, "benchmark all switches once and exit (no TUI)")
	slowMs := flag.Int64("slowms", 500, "log polls slower than this (ms); 0=disable")
	community := flag.String("c", "", "SNMP community string (overrides ~/.config/snmp.community)")
	swFile := flag.String("f", "/dev/shm/sw-ip-name-mac", "switch list file (IP NAME [MAC])")
	stateFile := flag.String("state", "/tmp/mifstat_go.bin", "state file to save history")
	vFlag := flag.Bool("version", false, "show version and exit")
	flag.Parse()

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
		runBenchmark(switches, sem)
		return
	}

	saved := loadState(*stateFile)
	states := make([]*SwitchData, len(switches))
	for i, sw := range switches {
		sd := &SwitchData{
			Name:           sw["name"],
			IP:             sw["ip"],
			Status:         "WAITING",
			Rates:          make(map[string]*PortRate),
			PortHist:       make(map[string]*PortHistory),
			SampleInterval: *delay,
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
	sortKey := "out"
	autoSort := map[string]bool{"main": true, "detail": true, "perf": true}
	viewMode := 0 // 0: Sparkline, 1: Braille, 2: Numeric
	zoomIdx := 0
	var viewNow *float64
	var prevItems []DisplayItem
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		// Determine which screen's sort state to use.
		currScreen := "main"
		if showPerf {
			currScreen = "perf"
		} else if showDetail {
			currScreen = "detail"
		}

		select {
		case ev := <-eventCh:
			switch e := ev.(type) {
			case *tcell.EventKey:
				zoom := zoomLevels[zoomIdx]
				switch {
				case e.Rune() == 'q':
					saveState(states, *stateFile)
					return
				case e.Rune() == 'p':
					showPerf = !showPerf
				case e.Rune() == 'd':
					showDetail = !showDetail
					showPerf = false
					prevItems = nil
				case e.Rune() == ' ':
					autoSort[currScreen] = !autoSort[currScreen]
				case e.Rune() == 'v':
					viewMode = (viewMode + 1) % 3
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

		if autoSort[currScreen] {
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

		renderMain(screen, items, h, w, delay, zoom, dispNow, revStyle, defStyle, dimStyle, autoSort[currScreen], viewNow, viewMode)
		screen.Show()
	}
}

func renderPerf(screen tcell.Screen, states []*SwitchData, h, w int, revStyle, warnStyle, defStyle, dimStyle tcell.Style, autoSort bool) {
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
		if l := len(r.IP); l > pwIP {
			pwIP = l
		}
		if l := len(r.Name); l > pwName {
			pwName = l
		}
		if l := len(r.Status); l > pwStatus {
			pwStatus = l
		}
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
	frozen := "[AUTO]"
	if !autoSort {
		frozen = "[FROZEN]"
	}
	statusLine := fmt.Sprintf("%s p:hide-perf  q:quit  (sorted by avg poll time; Phys=ethernetCsmacd+LAG, Ifaces=all SNMP)", frozen)
	drawStr(screen, 0, h-1, statusLine[:min(len(statusLine), w-1)], dimStyle)
	screen.Show()
}

func renderMain(screen tcell.Screen, items []DisplayItem, h, w int, delay *float64, zoom int, dispNow float64, revStyle, defStyle, dimStyle tcell.Style, autoSort bool, viewNow *float64, viewMode int) {
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

	if viewMode == 2 {
		hdr += getNumericHeader(sparkW)
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

		if viewMode == 2 {
			numStr := getNumericHistory(item.Hist, dispNow, sparkW, item.SampleInterval)
			drawStr(screen, xSpark, i+1, numStr, defStyle)
		} else {
			var sparkChars []rune
			var sparkStale []bool
			if viewMode == 1 {
				sparkChars, sparkStale = getBrailleSparkline(item.Hist, sparkW, *delay, zoom, dispNow, item.SampleInterval)
			} else {
				sparkChars, sparkStale = getSparkline(item.Hist, sparkW, *delay, zoom, dispNow, item.SampleInterval)
			}
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
	vModes := []string{"SPARK", "BRAIL", "NUMER"}
	delayStr := fmt.Sprintf("%.4g", *delay)
	statusLine := fmt.Sprintf("%s %s %sd=%ss zoom:1/%dx  q:quit d:detail v:view i/o:sort ARROWS:scroll ENTER:now",
		frozen, vModes[viewMode], scroll, delayStr, zoom)
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
