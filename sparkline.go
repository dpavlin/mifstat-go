package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

var zoomLevels = []int{1, 2, 5, 10, 30, 60, 120}

// getSparkline renders history as block-character sparkline cells.
// Returns (chars, staleFlags): stale cells should be rendered with dimStyle.
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
	effectivePeriod := math.Max(delay, sampleInterval)
	persistSec := math.Min(effectivePeriod*2.5*float64(zoom), float64(width)*pixelSec)

	data := make([]float64, width)
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
			idx = 4 // flat non-zero history → middle bar
		}
		if idx < 0 {
			idx = 0
		}
		if idx > 7 {
			idx = 7
		}
		chars[i] = blockChars[idx]
		staleFlags[i] = stale[i]
	}
	// Overlay age indicator at the right edge.
	for i, ch := range []rune(ageStr) {
		if sparkW+i < width {
			chars[sparkW+i] = ch
			staleFlags[sparkW+i] = true
		}
	}
	return
}

// getBrailleSparkline renders history as Braille characters (2 columns per cell).
func getBrailleSparkline(history []Sample, width int, delay float64, zoom int, now, sampleInterval float64) (chars []rune, staleFlags []bool) {
	width2 := width * 2
	chars = make([]rune, width)
	staleFlags = make([]bool, width)
	for i := range chars {
		chars[i] = ' '
	}
	if width <= 0 || len(history) == 0 {
		return
	}

	pixelSec := (delay * float64(zoom)) / 2.0
	startTime := now - float64(width2)*pixelSec

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

	effectivePeriod := math.Max(delay, sampleInterval)
	persistSec := math.Min(effectivePeriod*2.5*float64(zoom), float64(width2)*pixelSec)

	data := make([]float64, width2)
	valid := make([]bool, width2)
	for i := 0; i < width2; i++ {
		tp := now - float64(width2-1-i)*pixelSec
		tb := math.Floor(tp/pixelSec) * pixelSec
		if v, ok := buckets[tb]; ok {
			data[i], valid[i] = v, true
		} else {
			var lastT, lastV float64
			found := false
			for t, v := range buckets {
				if t < tp && (!found || t > lastT) {
					lastT, lastV, found = t, v, true
				}
			}
			if found && (tp-lastT) < persistSec {
				data[i], valid[i] = lastV, true
			}
		}
	}

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

	getDots := func(val float64, col int) int {
		h := 0
		if high > low {
			h = int(((val - low) / (high - low)) * 4)
		} else if val > 0 {
			h = 2
		}
		dots := 0
		if col == 0 {
			if h >= 1 { dots |= 0x40 }
			if h >= 2 { dots |= 0x04 }
			if h >= 3 { dots |= 0x02 }
			if h >= 4 { dots |= 0x01 }
		} else {
			if h >= 1 { dots |= 0x80 }
			if h >= 2 { dots |= 0x20 }
			if h >= 3 { dots |= 0x10 }
			if h >= 4 { dots |= 0x08 }
		}
		return dots
	}

	for i := 0; i < width; i++ {
		d1, d2 := 0, 0
		v1, v2 := false, false
		if valid[i*2] {
			d1 = getDots(data[i*2], 0)
			v1 = true
		}
		if valid[i*2+1] {
			d2 = getDots(data[i*2+1], 1)
			v2 = true
		}
		if v1 || v2 {
			chars[i] = rune(0x2800 | d1 | d2)
			if chars[i] == 0x2800 {
				chars[i] = ' '
			}
		}
	}
	return
}

func getNumericHistory(history []Sample, now float64) string {
	windows := []struct {
		name string
		sec  float64
	}{
		{"10s", 10},
		{"1m", 60},
		{"5m", 300},
		{"15m", 900},
	}

	var parts []string
	for _, w := range windows {
		sum, count := 0.0, 0
		start := now - w.sec
		for _, s := range history {
			if s.TS >= start && s.TS <= now {
				sum += s.Val
				count++
			}
		}
		val := 0.0
		if count > 0 {
			val = sum / float64(count)
		}
		parts = append(parts, fmt.Sprintf("%s:%5.1f", w.name, val))
	}
	return "[" + strings.Join(parts, " | ") + "]"
}

func getTrendHeader(width int, delay float64, zoom int, dispNow float64) string {
	if width < 20 {
		return ""
	}
	pixelSec := delay * float64(zoom)
	labels := []float64{0, 30, 60, 120, 300, 600, 1800, 3600, 7200, 14400, 21600}
	hdr := make([]rune, width)
	for i := range hdr {
		hdr[i] = ' '
	}

	now := float64(time.Now().UnixNano()) / 1e9
	offset := now - dispNow

	for _, s := range labels {
		pos := width - 1 - int((s+offset)/pixelSec)
		if pos < 0 || pos >= width {
			continue
		}
		var sLabel string
		if s == 0 {
			sLabel = "now"
		} else if s < 60 {
			sLabel = fmt.Sprintf("%.0fs", s)
		} else if s < 3600 {
			sLabel = fmt.Sprintf("%.0fm", s/60)
		} else {
			sLabel = fmt.Sprintf("%.0fh", s/3600)
		}
		for i, ch := range []rune(sLabel) {
			if pos+i < width {
				hdr[pos+i] = ch
			}
		}
	}
	return string(hdr)
}
