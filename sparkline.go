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
func getSparkline(history []Sample, width int, delay float64, zoom int, now, sampleInterval float64, lastPollMs int64) (chars []rune, staleFlags []bool) {
	chars = make([]rune, width)
	staleFlags = make([]bool, width)
	for i := range chars {
		chars[i] = ' '
	}
	if width <= 0 || len(history) == 0 {
		return
	}

	// At zoom 1x, ensure we don't have higher horizontal resolution than the switch can provide.
	effectiveDelay := delay
	if zoom == 1 {
		effectiveDelay = math.Max(delay, sampleInterval)
	}
	pixelSec := effectiveDelay * float64(zoom)
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
	// We use the actual switch sampleInterval to bridge gaps, plus a buffer.
	effectivePeriod := math.Max(delay, sampleInterval)
	persistSec := effectivePeriod * 2.5 * float64(zoom)
	// But don't stretch so far that we hide real long-term data loss.
	persistSec = math.Min(persistSec, 30.0*float64(zoom))

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

	// Status indicator: how many seconds since the last real sample OR latency.
	ageS := int(now - lastSampleT)
	var statusStr string
	isStale := ageS >= int(delay*2.5) || ageS > 5

	if isStale {
		if ageS >= 3600 {
			statusStr = fmt.Sprintf("%2dh", ageS/3600)
		} else if ageS >= 60 {
			statusStr = fmt.Sprintf("%2dm", ageS/60)
		} else {
			statusStr = fmt.Sprintf("%2ds", ageS)
		}
	} else {
		if lastPollMs < 1000 {
			statusStr = fmt.Sprintf("%3dm", lastPollMs) // "m" for ms to keep it short
		} else {
			statusStr = fmt.Sprintf("%.1fs", float64(lastPollMs)/1000.0)
		}
	}

	sparkW := width - len(statusStr)
	if sparkW < 0 {
		sparkW = 0
	}

	blockChars := []rune(" \u2581\u2582\u2583\u2584\u2585\u2586\u2587\u2588")
	for i := 0; i < sparkW; i++ {
		if !valid[i] {
			continue // leave as space
		}
		idx := 0
		if high > low {
			idx = 1 + int(((data[i]-low)/(high-low))*7)
		} else if data[i] > 0 {
			idx = 4 // flat non-zero history -> middle bar
		}
		
		if idx < 0 { idx = 0 }
		if idx > 8 { idx = 8 }
		
		if data[i] == 0 && valid[i] {
			chars[i] = ' '
		} else {
			chars[i] = blockChars[idx]
		}
		staleFlags[i] = stale[i]
	}
	// Overlay status indicator at the right edge.
	for i, ch := range []rune(statusStr) {
		if sparkW+i < width {
			chars[sparkW+i] = ch
			staleFlags[sparkW+i] = isStale
		}
	}
	return
}

// getNumericHeader generates dynamic intervals and column headers based on zoom and width.
func getNumericHeader(width int, delay float64, zoom int) string {
	if width < 10 {
		return ""
	}
	colW := 9 // Fixed width for numeric columns
	numCols := width / colW
	pixelSec := delay * float64(zoom)

	var sb strings.Builder
	for i := 0; i < numCols; i++ {
		sec := float64(i) * pixelSec
		var label string
		if i == 0 {
			label = "Now"
		} else if sec < 60 {
			label = fmt.Sprintf("-%.0fs", sec)
		} else if sec < 3600 {
			label = fmt.Sprintf("-%.0fm", sec/60)
		} else {
			label = fmt.Sprintf("-%.1fh", sec/3600)
		}
		s := fmt.Sprintf("%*s", colW, label)
		sb.WriteString(s)
	}
	return sb.String()
}

func getNumericHistory(history []Sample, now float64, width int, delay float64, zoom int, sampleInterval float64) string {
	if width < 10 {
		return ""
	}
	colW := 9
	numCols := width / colW
	pixelSec := delay * float64(zoom)

	var sb strings.Builder
	for i := 0; i < numCols; i++ {
		targetTS := now - float64(i)*pixelSec
		val := -1.0
		// Find closest sample <= targetTS
		for j := len(history) - 1; j >= 0; j-- {
			if history[j].TS <= targetTS {
				// Persist logic: only show value if it's within a reasonable window of the target time
				persistLimit := math.Max(60.0, pixelSec*1.5)
				if (targetTS - history[j].TS) < persistLimit {
					val = history[j].Val
				}
				break
			}
		}

		var s string
		if val < 0 {
			s = fmt.Sprintf("%*s", colW, "-")
		} else {
			// Format using simpler compact version for the grid to fit in colW (9)
			s = fmt.Sprintf("%*s", colW, formatRateCompact(val))
		}
		sb.WriteString(s)
	}
	return sb.String()
}

func formatRateCompact(kbps float64) string {
	if kbps >= 1024*1024 {
		return fmt.Sprintf("%.1fG", kbps/(1024*1024))
	}
	if kbps >= 1024 {
		return fmt.Sprintf("%.1fM", kbps/1024)
	}
	if kbps >= 1 {
		return fmt.Sprintf("%.1fK", kbps)
	}
	return "0"
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
			targetPos := pos + i - (len(sLabel) - 1)
			if targetPos >= 0 && targetPos < width {
				hdr[targetPos] = ch
			}
		}
	}
	return string(hdr)
}
