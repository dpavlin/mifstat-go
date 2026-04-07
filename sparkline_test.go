package main

import (
	"strings"
	"testing"
)

func TestGetNumericHeader(t *testing.T) {
	width := 60
	header := getNumericHeader(width)
	
	if len(header) < width {
		t.Errorf("header length %d, want at least %d", len(header), width)
	}

	for _, iv := range numericIntervals {
		if !strings.Contains(header, iv.label) {
			t.Errorf("header missing interval label: %q", iv.label)
		}
	}
}

func TestGetNumericHistory(t *testing.T) {
	width := 60
	now := 1000.0
	sampleInterval := 1.0
	history := []Sample{
		{TS: 100.0, Val: 5.0}, // Very old
		{TS: 700.0, Val: 10.0},
		{TS: 940.0, Val: 40.0},
		{TS: 970.0, Val: 70.0},
		{TS: 990.0, Val: 90.0},
		{TS: 1000.0, Val: 100.0},
	}

	histStr := getNumericHistory(history, now, width, sampleInterval)
	if len(histStr) < width {
		t.Errorf("history string length %d, want %d", len(histStr), width)
	}

	// Should contain "100.0" for Now
	if !strings.Contains(histStr, "100.0") {
		t.Errorf("numeric history missing current value: %q", histStr)
	}
	// Should contain "90.0" for -10s
	if !strings.Contains(histStr, "90.0") {
		t.Errorf("numeric history missing -10s value: %q", histStr)
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

func TestGetBrailleSparkline(t *testing.T) {
	history := []Sample{
		{TS: 100.0, Val: 10.0},
		{TS: 100.5, Val: 20.0},
	}
	width := 10
	delay := 1.0
	zoom := 1
	now := 105.0
	sampleInterval := 1.0

	chars, stale := getBrailleSparkline(history, width, delay, zoom, now, sampleInterval)
	if len(chars) != width {
		t.Errorf("braille sparkline length %d, want %d", len(chars), width)
	}
	if len(stale) != width {
		t.Errorf("stale flags length %d, want %d", len(stale), width)
	}
}
