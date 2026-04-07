package main

import (
	"strings"
	"testing"
	"time"
)

func TestGetNumericHeader(t *testing.T) {
	width := 60
	delay := 1.0
	zoom := 1
	header := getNumericHeader(width, delay, zoom)
	
	if len(header) < width {
		// getNumericHeader might return slightly less than width due to colW=9
		expected := (width / 9) * 9
		if len(header) != expected {
			t.Errorf("header length %d, want %d", len(header), expected)
		}
	}

	if !strings.Contains(header, "Now") {
		t.Errorf("header missing 'Now' label: %q", header)
	}
}

func TestGetNumericHistory(t *testing.T) {
	width := 60
	now := 1000.0
	delay := 1.0
	zoom := 1
	sampleInterval := 1.0
	history := []Sample{
		{TS: 100.0, Val: 5.0}, // Very old
		{TS: 700.0, Val: 10.0},
		{TS: 940.0, Val: 40.0},
		{TS: 970.0, Val: 70.0},
		{TS: 990.0, Val: 90.0},
		{TS: 1000.0, Val: 100.0},
	}

	histStr := getNumericHistory(history, now, width, delay, zoom, sampleInterval)
	
	// Should contain "100.0K" (formatRateCompact) for Now
	if !strings.Contains(histStr, "100.0K") {
		t.Errorf("numeric history missing current value: %q", histStr)
	}
	// Should contain "90.0K" for -10s
	if !strings.Contains(histStr, "90.0K") {
		t.Errorf("numeric history missing -10s value: %q", histStr)
	}
}

func TestFormatRateCompact(t *testing.T) {
	tests := []struct {
		rate     float64
		expected string
	}{
		{0.0, "0"},
		{0.5, "0"},
		{1.5, "1.5K"},
		{1024.0, "1.0M"},
		{1536.0, "1.5M"},
		{1048576.0, "1.0G"},
	}

	for _, tc := range tests {
		actual := formatRateCompact(tc.rate)
		if actual != tc.expected {
			t.Errorf("formatRateCompact(%.2f) = %q; want %q", tc.rate, actual, tc.expected)
		}
	}
}

func TestGetTrendHeader(t *testing.T) {
	width := 50
	delay := 1.0
	zoom := 1
	now := float64(time.Now().UnixNano()) / 1e9
	
	header := getTrendHeader(width, delay, zoom, now)
	if len(header) != width {
		t.Errorf("trend header length %d, want %d", len(header), width)
	}
	if !strings.Contains(header, "now") {
		t.Errorf("trend header missing 'now': %q", header)
	}
}

func TestGetSparkline(t *testing.T) {
	history := []Sample{
		{TS: 100.0, Val: 10.0},
		{TS: 101.0, Val: 20.0},
		{TS: 102.0, Val: 30.0},
	}
	width := 10
	delay := 1.0
	zoom := 1
	now := 105.0
	sampleInterval := 1.0

	chars, stale := getSparkline(history, width, delay, zoom, now, sampleInterval, 45)
	if len(chars) != width {
		t.Errorf("sparkline length %d, want %d", len(chars), width)
	}
	if len(stale) != width {
		t.Errorf("stale flags length %d, want %d", len(stale), width)
	}
}

func TestGetSparklineStatus(t *testing.T) {
	history := []Sample{{TS: 1000.0, Val: 10.0}}
	width := 20
	delay := 1.0
	zoom := 1
	sampleInterval := 1.0

	// Case 1: Fresh data (age = 0s), should show latency
	nowFresh := 1000.0
	latency := int64(45)
	chars, _ := getSparkline(history, width, delay, zoom, nowFresh, sampleInterval, latency)
	res := string(chars)
	if !strings.Contains(res, "45m") {
		t.Errorf("fresh sparkline should contain latency '45m', got %q", res)
	}

	// Case 2: Stale data (age = 10s), should show age
	nowStale := 1010.0
	charsS, _ := getSparkline(history, width, delay, zoom, nowStale, sampleInterval, latency)
	resS := string(charsS)
	if !strings.Contains(resS, "10s") {
		t.Errorf("stale sparkline should contain age '10s', got %q", resS)
	}
}

func TestGetSparklineAdaptiveGap(t *testing.T) {
	// Global delay 1 but switch responds every 2s
	delay := 1.0
	sampleInterval := 2.0
	now := 1000.0
	// Provide samples that cross multiple 1s pixels.
	history := []Sample{
		{TS: 980.0, Val: 10.0},
		{TS: 982.0, Val: 10.0},
		{TS: 984.0, Val: 10.0},
		{TS: 986.0, Val: 10.0},
	}
	width := 30
	zoom := 1

	chars, _ := getSparkline(history, width, delay, zoom, now, sampleInterval, 50)
	res := string(chars)
	
	// Check continuity between the first and last rendered data point (excluding space)
	charsOnly := "▂▃▄▅▆▇█"
	first := strings.IndexAny(res, charsOnly)
	last := strings.LastIndexAny(res, charsOnly)
	
	if first == -1 {
		t.Fatalf("sparkline rendered no data points, got %q", res)
	}
	
	dataPart := res[first : last+1]
	// With the new logic, gaps are filled based on sampleInterval (2s), 
	// even if the pixel step is smaller (1s).
	if strings.Contains(dataPart, " ") {
		t.Errorf("sparkline data part should be continuous due to gap filling, got %q (full: %q)", dataPart, res)
	}
}

func TestGetSparklineLowTraffic(t *testing.T) {
	// One massive spike and one tiny value. Tiny value should NOT be a space.
	history := []Sample{
		{TS: 90.0, Val: 1000.0}, // The spike
		{TS: 91.0, Val: 1.0},    // Tiny value
	}
	width := 20
	delay := 1.0
	zoom := 1
	now := 102.0
	sampleInterval := 1.0

	chars, _ := getSparkline(history, width, delay, zoom, now, sampleInterval, 50)
	res := string(chars)
	
	// The tiny value at TS=101.0 should map to a pixel before the latency.
	// It must be one of the block characters, not ' '.
	foundTiny := false
	for _, r := range chars {
		if r != ' ' && r != '█' && r != '5' && r != '0' && r != 'm' {
			foundTiny = true
			break
		}
	}
	if !foundTiny {
		t.Errorf("tiny traffic value should be visible, sparkline was %q", res)
	}
}
