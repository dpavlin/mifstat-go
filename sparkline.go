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
func getSparkline(timestamps []float64, history, latHistory []float32, width int, delay float64, zoom int, now, sampleInterval float64, lastPollMs int64, slowMs int64) (chars []rune, staleFlags []bool) {
	chars = make([]rune, width)
	staleFlags = make([]bool, width)
	for i := range chars {
		chars[i] = ' '
	}
	if width <= 0 || len(history) == 0 {
		return
	}

	// USE GLOBAL UNIFIED TIME STEP FOR VERTICAL ALIGNMENT.
	// One pixel MUST represent the same amount of time for all rows.
	pixelSec := delay * float64(zoom)
	startTime := now - float64(width)*pixelSec

	// Build buckets: aligned time slot → max value in that slot.
	buckets := make(map[float64]float32)
	for i, ts := range timestamps {
		if i >= len(history) { break }
		if ts < startTime {
			continue
		}
		tb := math.Floor(ts/pixelSec) * pixelSec
		v, ok := buckets[tb]
		if !ok {
			buckets[tb] = history[i]
		} else {
			// Special handling for errors: if any sample in bucket is an error, show it as an error.
			if history[i] == -1.0 || v == -1.0 {
				buckets[tb] = -1.0
			} else if history[i] > v {
				buckets[tb] = history[i]
			}
		}
	}

	// Build latency buckets: true if any poll in that bucket was "slow"
	slowBuckets := make(map[float64]bool)
	if slowMs > 0 {
		for i, ts := range timestamps {
			if i >= len(latHistory) { break }
			if ts < startTime {
				continue
			}
			if latHistory[i] > float32(slowMs) {
				tb := math.Floor(ts/pixelSec) * pixelSec
				slowBuckets[tb] = true
			}
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

	data := make([]float32, width)
	valid := make([]bool, width)
	stale := make([]bool, width) // true = pixel is in the right-edge gap

	for i := 0; i < width; i++ {
		tp := now - float64(width-1-i)*pixelSec
		tb := math.Floor(tp/pixelSec) * pixelSec
		if v, ok := buckets[tb]; ok {
			data[i], valid[i] = v, true
		} else {
			// Find most recent sample strictly before this pixel.
			var lastT float64
			var lastV float32
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
	high, low := float32(1.0), float32(0.0)
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

	// Use more characters for higher vertical resolution.
	// We add '.' (bottom dot) and '_' (bottom line) as sub-1/8th increments.
	blockChars := []rune(" ._\u2581\u2582\u2583\u2584\u2585\u2586\u2587\u2588")
	numLevels := len(blockChars) - 1 // 10 levels above space

	for i := 0; i < sparkW; i++ {
		if !valid[i] {
			continue // leave as space
		}
		idx := 0
		if high > low {
			// Map data to range [1, numLevels]
			idx = 1 + int(((data[i]-low)/(high-low))*float32(numLevels-1))
		} else if data[i] > 0 {
			idx = numLevels / 2 // flat non-zero history -> middle bar
		}
		
		if idx < 0 { idx = 0 }
		if idx > numLevels { idx = numLevels }
		
		tp := now - float64(width-1-i)*pixelSec
		tb := math.Floor(tp/pixelSec) * pixelSec
		
		if data[i] == -1.0 {
			chars[i] = '!'
		} else if slowBuckets[tb] {
			chars[i] = '*'
		} else if data[i] == 0 && valid[i] {
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

func getNumericHistory(timestamps []float64, history []float32, now float64, width int, delay float64, zoom int, sampleInterval float64) string {
	if width < 10 || len(history) == 0 {
		return ""
	}
	colW := 9
	numCols := width / colW
	pixelSec := delay * float64(zoom)

	var sb strings.Builder
	for i := 0; i < numCols; i++ {
		targetTS := now - float64(i)*pixelSec
		val := float32(-1.0)
		// Find closest sample <= targetTS
		for j := len(timestamps) - 1; j >= 0; j-- {
			if j >= len(history) { continue }
			if timestamps[j] <= targetTS {
				// Persist logic: only show value if it's within a reasonable window of the target time
				persistLimit := math.Max(60.0, pixelSec*1.5)
				if (targetTS - timestamps[j]) < persistLimit {
					val = history[j]
				}
				break
			}
		}

		var s string
		if val < 0 {
			s = fmt.Sprintf("%*s", colW, "-")
		} else {
			// Format using simpler compact version for the grid to fit in colW (9)
			s = fmt.Sprintf("%*s", colW, formatRateCompact(float64(val)))
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
