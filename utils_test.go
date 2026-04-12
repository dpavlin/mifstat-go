package main

import (
	"testing"
)

func TestFormatRate(t *testing.T) {
	tests := []struct {
		rate     float64
		expected string
	}{
		{0.0, "0.00 KB/s"},
		{500.5, "500.50 KB/s"},
		{1024.0, "1.00 MB/s"},
		{1536.0, "1.50 MB/s"},
		{1048576.0, "1.00 GB/s"},
		{1572864.0, "1.50 GB/s"},
	}

	for _, tc := range tests {
		actual := formatRate(tc.rate)
		if actual != tc.expected {
			t.Errorf("formatRate(%.2f) = %q; want %q", tc.rate, actual, tc.expected)
		}
	}
}

func TestCalcEMA(t *testing.T) {
	alpha := 0.1
	tests := []struct {
		current  float64
		prev     float64
		expected float64
	}{
		{100.0, 0.0, 100.0},   // Initial value should jump to current
		{110.0, 100.0, 101.0}, // 110*0.1 + 100*0.9 = 11.0 + 90.0 = 101.0
		{50.0, 100.0, 95.0},   // 50*0.1 + 100*0.9 = 5.0 + 90.0 = 95.0
	}

	for _, tc := range tests {
		actual := calcEMA(tc.current, tc.prev, alpha)
		if actual != tc.expected {
			t.Errorf("calcEMA(%.2f, %.2f) = %.2f; want %.2f", tc.current, tc.prev, actual, tc.expected)
		}
	}
}

func TestMin(t *testing.T) {
	if min(1, 2) != 1 {
		t.Errorf("min(1, 2) = %d; want 1", min(1, 2))
	}
	if min(2, 1) != 1 {
		t.Errorf("min(2, 1) = %d; want 1", min(2, 1))
	}
	if min(1, 1) != 1 {
		t.Errorf("min(1, 1) = %d; want 1", min(1, 1))
	}
}
