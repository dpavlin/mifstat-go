package main

import (
	"testing"
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
		{".1.3.6.1.2.1.2.2.1.3.42", "1.3.6.1.2.1.2.2.1.3", 42, true}, // leading dot mismatch handling
		{"1.3.6.1.2.1.2.2.1.3.42", ".1.3.6.1.2.1.2.2.1.3", 42, true}, // leading dot mismatch handling
		{".1.3.6.1.2.1.2.2.1.4.15", "1.3.6.1.2.1.2.2.1.3", 0, false}, // wrong root
		{"invalid", "1.3", 0, false},
	}

	for _, tc := range tests {
		idx, ok := oidIndex(tc.oid, tc.root)
		if idx != tc.expected || ok != tc.ok {
			t.Errorf("oidIndex(%q, %q) = %d, %v; want %d, %v", tc.oid, tc.root, idx, ok, tc.expected, tc.ok)
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
	expected := 2 // Only types 6 and 161 are considered physical
	
	if count := countPhys(m); count != expected {
		t.Errorf("countPhys() = %d; want %d", count, expected)
	}
}
