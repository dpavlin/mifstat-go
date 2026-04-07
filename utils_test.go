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
