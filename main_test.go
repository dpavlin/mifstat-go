package main

import "testing"

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name, ip, filter string
		want             bool
	}{
		{"switch1", "10.0.0.1", "", true},
		{"switch1", "10.0.0.1", "switch", true},
		{"switch1", "10.0.0.1", "10.0", true},
		{"switch1", "10.0.0.1", "other", false},
		{"CORE-SW", "192.168.1.1", "core", true},
		{"CORE-SW", "192.168.1.1", "192", true},
	}

	for _, tc := range tests {
		if got := matchesFilter(tc.name, tc.ip, tc.filter); got != tc.want {
			t.Errorf("matchesFilter(%q, %q, %q) = %v; want %v", tc.name, tc.ip, tc.filter, got, tc.want)
		}
	}
}
