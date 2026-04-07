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

	chars, stale := getSparkline(history, width, delay, zoom, now, sampleInterval)
	if len(chars) != width {
		t.Errorf("sparkline length %d, want %d", len(chars), width)
	}
	if len(stale) != width {
		t.Errorf("stale flags length %d, want %d", len(stale), width)
	}
}
