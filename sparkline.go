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
func getSparkline(timestamps *Float64Ring, history, latHistory *Float32Ring, width int, delay float64, zoom int, now, sampleInterval float64, lastPollMs int64, slowMs int64) (chars []rune, staleFlags []bool) {
	chars = make([]rune, width)
	staleFlags = make([]bool, width)
	for i := range chars {
		chars[i] = ' '
	}
	if width <= 0 || history.Len == 0 {
		return
	}

	pixelSec := delay * float64(zoom)
	startTime := now - float64(width)*pixelSec

	data := make([]float32, width)
	valid := make([]bool, width)
	slow := make([]bool, width)
	var lastSampleT float64 = -1

	// Single pass over the ring buffers to fill buckets.
	// This is O(N) where N is history samples, replacing the O(Pixels * History) or O(Pixels * Map) logic.
	for i := 0; i < timestamps.Len; i++ {
		ts := timestamps.Get(i)
		if ts < startTime {
			continue
		}
		if ts > lastSampleT {
			lastSampleT = ts
		}

		// Map timestamp to pixel index [0, width-1]
		pixelIdx := int((ts - startTime) / pixelSec)
		if pixelIdx < 0 || pixelIdx >= width {
			continue
		}

		val := history.Get(i)
		// Error Priority: any error in bucket makes the pixel an error.
		if val == -1.0 {
			data[pixelIdx] = -1.0
		} else if data[pixelIdx] != -1.0 {
			if val > data[pixelIdx] || !valid[pixelIdx] {
				data[pixelIdx] = val
			}
		}
		valid[pixelIdx] = true

		if slowMs > 0 && latHistory != nil && i < latHistory.Len {
			if latHistory.Get(i) > float32(slowMs) {
				slow[pixelIdx] = true
			}
		}
	}

	if lastSampleT == -1 {
		return
	}

	// persistSec: how far forward to carry a sample value to fill inter-sample gaps.
	effectivePeriod := math.Max(delay, sampleInterval)
	persistSec := effectivePeriod * 2.5 * float64(zoom)
	persistSec = math.Min(persistSec, 30.0*float64(zoom))

	stale := make([]bool, width)

	// Gap filling and staleness logic.
	// We track the last known valid sample to bridge gaps without nested loops.
	var lastVal float32
	var lastValT float64
	var hasLast bool

	for i := 0; i < width; i++ {
		tp := startTime + float64(i)*pixelSec
		if valid[i] {
			lastVal = data[i]
			lastValT = tp // approximated to bucket start
			hasLast = true
		} else if hasLast && (tp-lastValT) < persistSec {
			data[i] = lastVal
			valid[i] = true
			if tp > lastSampleT {
				stale[i] = true
			}
		}
	}

	// Normalise for rendering.
	high, low := float32(1.0), float32(0.0)
	first := true
	for i, v := range data {
		if !valid[i] {
			continue
		}
		if first {
			high, low, first = v, v, false
		} else {
			if v > high { high = v }
			if v < low { low = v }
		}
	}

	// Status indicator.
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
			statusStr = fmt.Sprintf("%3dm", lastPollMs)
		} else {
			statusStr = fmt.Sprintf("%.1fs", float64(lastPollMs)/1000.0)
		}
	}

	sparkW := width - len(statusStr)
	if sparkW < 0 { sparkW = 0 }

	blockChars := []rune(" ._\u2581\u2582\u2583\u2584\u2585\u2586\u2587\u2588")
	numLevels := len(blockChars) - 1

	for i := 0; i < sparkW; i++ {
		if !valid[i] {
			continue
		}
		idx := 0
		if high > low {
			idx = 1 + int(((data[i]-low)/(high-low))*float32(numLevels-1))
		} else if data[i] > 0 {
			idx = numLevels / 2
		}
		if idx < 0 { idx = 0 }
		if idx > numLevels { idx = numLevels }
		
		if data[i] == -1.0 {
			chars[i] = '!'
		} else if slow[i] {
			chars[i] = '*'
		} else if data[i] == 0 {
			chars[i] = ' '
		} else {
			chars[i] = blockChars[idx]
		}
		staleFlags[i] = stale[i]
	}
	for i, ch := range []rune(statusStr) {
		if sparkW+i < width {
			chars[sparkW+i] = ch
			staleFlags[sparkW+i] = isStale
		}
	}
	return
}

func getNumericHeader(width int, delay float64, zoom int) string {
	if width < 10 {
		return ""
	}
	colW := 9
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

func getNumericHistory(timestamps *Float64Ring, history *Float32Ring, now float64, width int, delay float64, zoom int, sampleInterval float64) string {
	if width < 10 || history.Len == 0 {
		return ""
	}
	colW := 9
	numCols := width / colW
	pixelSec := delay * float64(zoom)

	var sb strings.Builder
	for i := 0; i < numCols; i++ {
		targetTS := now - float64(i)*pixelSec
		val := float32(-1.0)
		// Optimization: search backwards starting from the last index to find the closest sample.
		for j := timestamps.Len - 1; j >= 0; j-- {
			ts := timestamps.Get(j)
			if ts <= targetTS {
				persistLimit := math.Max(60.0, pixelSec*1.5)
				if (targetTS - ts) < persistLimit {
					val = history.Get(j)
				}
				break
			}
		}

		var s string
		if val < 0 {
			s = fmt.Sprintf("%*s", colW, "-")
		} else {
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
