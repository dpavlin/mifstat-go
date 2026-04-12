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
	
	if len(header) == 0 {
		t.Errorf("header is empty")
	}
	if !strings.Contains(strings.ToLower(header), "now") {
		t.Error("header should contain 'now'")
	}
}

func TestGetNumericHistory(t *testing.T) {
	now := 105.0
	delay := 1.0
	zoom := 1
	width := 40
	sampleInterval := 1.0
	timestamps := []float64{100.0, 101.0, 102.0, 103.0, 104.0, 105.0}
	history := []float32{10, 20, 30, 40, 50, 60}
	
	histStr := getNumericHistory(timestamps, history, now, width, delay, zoom, sampleInterval)
	
	// We expect the most recent values at the left
	if !strings.Contains(histStr, "60") {
		t.Errorf("history should contain '60', got %q", histStr)
	}
}

func TestFormatRateCompact(t *testing.T) {
	tests := []struct {
		rate     float64
		expected string
	}{
		{0.0, "0"},
		{500.5, "500.5K"},
		{1024.0, "1.0M"},
		{1024.0 * 1024.0 * 1.5, "1.5G"},
	}

	for _, tc := range tests {
		if got := formatRateCompact(tc.rate); got != tc.expected {
			t.Errorf("formatRateCompact(%f) = %q; want %q", tc.rate, got, tc.expected)
		}
	}
}

func TestGetTrendHeader(t *testing.T) {
	width := 60
	delay := 1.0
	zoom := 1
	now := float64(time.Now().UnixNano()) / 1e9
	header := getTrendHeader(width, delay, zoom, now)
	if len(header) != width {
		t.Errorf("header length %d, want %d", len(header), width)
	}
}

func TestGetSparkline(t *testing.T) {
	width := 40
	delay := 1.0
	zoom := 1
	now := 105.0
	sampleInterval := 1.0
	timestamps := []float64{100.0, 101.0, 102.0, 103.0, 104.0, 105.0}
	history := []float32{10, 20, 30, 40, 50, 60}

	chars, stale := getSparkline(timestamps, history, nil, width, delay, zoom, now, sampleInterval, 0, 0)
	if len(chars) != width {
		t.Errorf("sparkline length %d, want %d", len(chars), width)
	}
	if len(stale) != width {
		t.Errorf("stale flags length %d, want %d", len(stale), width)
	}
}

func TestGetSparklineStatus(t *testing.T) {
	width := 20
	delay := 1.0
	zoom := 1
	sampleInterval := 1.0

	// Case 1: Fresh data (age = 0s), should show latency
	nowFresh := 1000.0
	timestamps := []float64{1000.0}
	history := []float32{10.0}
	latency := int64(45)
	chars, _ := getSparkline(timestamps, history, nil, width, delay, zoom, nowFresh, sampleInterval, latency, 0)
	res := string(chars)
	if !strings.Contains(res, "45m") {
		t.Errorf("fresh sparkline should contain latency '45m', got %q", res)
	}

	// Case 2: Stale data (age = 10s), should show age
	nowStale := 1010.0
	charsS, _ := getSparkline(timestamps, history, nil, width, delay, zoom, nowStale, sampleInterval, latency, 0)
	resS := string(charsS)
	if !strings.Contains(resS, "10s") {
		t.Errorf("stale sparkline should contain age '10s', got %q", resS)
	}
}

func TestGetSparklineAdaptiveGap(t *testing.T) {
	delay := 1.0
	sampleInterval := 2.0
	now := 105.0
	width := 30
	zoom := 1

	// Switch responds every 2s
	timestamps := []float64{100.0, 102.0, 104.0}
	history := []float32{10, 20, 30}

	chars, _ := getSparkline(timestamps, history, nil, width, delay, zoom, now, sampleInterval, 0, 0)
	res := string(chars)

	// Check continuity between the first and last rendered data point
	foundAny := false
	for _, r := range chars {
		if r != ' ' && r != '0' && r != 'm' {
			foundAny = true
			break
		}
	}
	if !foundAny {
		t.Errorf("sparkline with gaps should still show data, got %q", res)
	}
}

func TestGetSparklineLowTraffic(t *testing.T) {
	width := 40
	delay := 1.0
	zoom := 1
	now := 105.0
	sampleInterval := 1.0
	timestamps := []float64{100.0}
	history := []float32{100.0}

	chars, _ := getSparkline(timestamps, history, nil, width, delay, zoom, now, sampleInterval, 0, 0)
	res := string(chars)

	foundData := false
	for _, r := range chars {
		if r != ' ' && r != '0' && r != 'm' {
			foundData = true
			break
		}
	}
	if !foundData {
		t.Errorf("traffic value should be visible, sparkline was %q", res)
	}
}

func TestSparklineResolution(t *testing.T) {
	history := []float32{100.0, 1.0, 15.0}
	timestamps := []float64{90.0, 89.0, 88.0}

	width := 40
	delay := 1.0
	zoom := 1
	now := 100.0
	sampleInterval := 1.0

	chars, _ := getSparkline(timestamps, history, nil, width, delay, zoom, now, sampleInterval, 0, 0)
	res := string(chars)

	t.Logf("Sparkline: %q", res)
	containsDot := false
	containsUnderscore := false
	containsFullBlock := false

	for _, r := range chars {
		if r == '.' { containsDot = true }
		if r == '_' { containsUnderscore = true }
		if r == '█' { containsFullBlock = true }
	}

	if !containsDot { t.Errorf("should contain '.', got %q", res) }
	if !containsUnderscore { t.Errorf("should contain '_', got %q", res) }
	if !containsFullBlock { t.Errorf("should contain '█', got %q", res) }
}

func TestSparklineErrors(t *testing.T) {
	history := []float32{100.0, -1.0, 50.0}
	timestamps := []float64{90.0, 85.0, 80.0}

	width := 40
	delay := 1.0
	zoom := 1
	now := 100.0
	sampleInterval := 1.0

	chars, _ := getSparkline(timestamps, history, nil, width, delay, zoom, now, sampleInterval, 0, 0)
	res := string(chars)

	foundError := false
	for _, r := range chars {
		if r == '!' {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Errorf("Sparkline should contain '!' for error samples (-1.0), got %q", res)
	}
}

func TestSparklineDelays(t *testing.T) {
	history := []float32{100.0, 50.0, 20.0}
	timestamps := []float64{90.0, 85.0, 80.0}
	latencies := []float32{10.0, 800.0, 10.0} // Slow poll (> 500ms)

	width := 40
	delay := 1.0
	zoom := 1
	now := 100.0
	sampleInterval := 1.0

	chars, _ := getSparkline(timestamps, history, latencies, width, delay, zoom, now, sampleInterval, 0, 500)
	res := string(chars)

	foundSlow := false
	for _, r := range chars {
		if r == '*' {
			foundSlow = true
			break
		}
	}

	if !foundSlow {
		t.Errorf("Sparkline should contain '*' for slow samples (>500ms), got %q", res)
	}
}
