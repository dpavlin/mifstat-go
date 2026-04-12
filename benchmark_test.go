package main

import (
	"testing"
)

func TestBenchmarkSlowCount(t *testing.T) {
	tests := []struct {
		snmpMs   int64
		slowMs   int64
		expected int
	}{
		{400, 500, 0},
		{600, 500, 1},
		{500, 500, 0}, // threshold is exclusive
		{1000, 800, 1},
	}

	for _, tc := range tests {
		count := 0
		if tc.slowMs > 0 && tc.snmpMs > tc.slowMs {
			count++
		}
		if count != tc.expected {
			t.Errorf("For snmpMs=%d, slowMs=%d: expected count %d, got %d", tc.snmpMs, tc.slowMs, tc.expected, count)
		}
	}
}
