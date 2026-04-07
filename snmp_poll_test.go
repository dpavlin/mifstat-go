package main

import (
	"testing"
	"github.com/gosnmp/gosnmp"
)

func TestOidIndex(t *testing.T) {
	tests := []struct {
		oid      string
		root     string
		expected int
		ok       bool
	}{
		{".1.3.6.1.2.1.2.2.1.3.15", ".1.3.6.1.2.1.2.2.1.3", 15, true},
		{"1.3.6.1.2.1.2.2.1.3.42", "1.3.6.1.2.1.2.2.1.3", 42, true},
		{".1.3.6.1.2.1.2.2.1.3.42", "1.3.6.1.2.1.2.2.1.3", 42, true},
		{"1.3.6.1.2.1.2.2.1.3.42", ".1.3.6.1.2.1.2.2.1.3", 42, true},
		{".1.3.6.1.2.1.2.2.1.4.15", "1.3.6.1.2.1.2.2.1.3", 0, false},
		{"invalid", "1.3", 0, false},
	}

	for _, tc := range tests {
		idx, ok := oidIndex(tc.oid, tc.root)
		if idx != tc.expected || ok != tc.ok {
			t.Errorf("oidIndex(%q, %q) = %d, %v; want %d, %v", tc.oid, tc.root, idx, ok, tc.expected, tc.ok)
		}
	}
}

func TestPruneSamples(t *testing.T) {
	now := 10000.0
	history := []Sample{
		{TS: now - MAX_HIST_SEC - 10, Val: 1.0},
		{TS: now - MAX_HIST_SEC - 5, Val: 2.0},
		{TS: now - MAX_HIST_SEC + 5, Val: 3.0},
		{TS: now, Val: 4.0},
	}

	pruned := pruneSamples(history, now)
	if len(pruned) != 2 {
		t.Errorf("pruneSamples returned %d items; want 2", len(pruned))
	}
	if pruned[0].Val != 3.0 {
		t.Errorf("first sample val %f; want 3.0", pruned[0].Val)
	}
}

func TestSnmpUint64(t *testing.T) {
	tests := []struct {
		val      interface{}
		expected uint64
	}{
		{uint32(42), 42},
		{uint64(1234567890123), 1234567890123},
		{int(100), 100},
		{"string", 0},
	}

	for _, tc := range tests {
		pdu := gosnmp.SnmpPDU{Value: tc.val}
		actual := snmpUint64(pdu)
		if actual != tc.expected {
			t.Errorf("snmpUint64(%v) = %d; want %d", tc.val, actual, tc.expected)
		}
	}
}

func TestCountPhys(t *testing.T) {
	m := map[int]int{
		1: 6,   // ethernetCsmacd
		2: 24,  // softwareLoopback
		3: 161, // ieee8023adLag
		4: 53,  // propVirtual
	}
	expected := 2
	
	if count := countPhys(m); count != expected {
		t.Errorf("countPhys() = %d; want %d", count, expected)
	}
}
