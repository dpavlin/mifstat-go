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
		{".1.3.6.1.2.1.31.1.1.1.6.1", "1.3.6.1.2.1.31.1.1.1.6", 1, true},
		{"1.3.6.1.2.1.31.1.1.1.6.10", "1.3.6.1.2.1.31.1.1.1.6", 10, true},
		{"1.3.6.1.2.1.31.1.1.1.6.1", ".1.3.6.1.2.1.31.1.1.1.6", 1, true},
		{"1.3.6.1.2.1.31.1.1.1.10.5", "1.3.6.1.2.1.31.1.1.1.6", 0, false},
	}

	for _, tc := range tests {
		got, ok := oidIndex(tc.oid, tc.root)
		if ok != tc.ok || got != tc.expected {
			t.Errorf("oidIndex(%q, %q) = (%d, %v); want (%d, %v)", tc.oid, tc.root, got, ok, tc.expected, tc.ok)
		}
	}
}

func TestSnmpUint64(t *testing.T) {
	tests := []struct {
		val      interface{}
		expected uint64
	}{
		{uint32(100), 100},
		{uint64(200), 200},
		{int(300), 300},
		{uint(400), 400},
		{"invalid", 0},
	}

	for _, tc := range tests {
		pdu := gosnmp.SnmpPDU{Value: tc.val}
		if got := snmpUint64(pdu); got != tc.expected {
			t.Errorf("snmpUint64(%v) = %d; want %d", tc.val, got, tc.expected)
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
