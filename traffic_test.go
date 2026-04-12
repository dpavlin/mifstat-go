package main

import (
	"testing"
)

func TestTrafficMaxTracking(t *testing.T) {
	sw := &SwitchData{
		Rates: make(map[string]*PortRate),
		Timestamps: NewFloat64Ring(10),
		HistIn: NewFloat32Ring(10),
		HistOut: NewFloat32Ring(10),
	}
	
	now := 100000.0
	delay := 1.0
	
	// First poll: initialize counters
	tables1 := map[string]map[int]uint64{
		OID_HCIN:  {1: 1000},
		OID_HCOUT: {1: 2000},
	}
	processSamples(sw, tables1, now, delay)
	
	if sw.MaxIn != 0 || sw.MaxOut != 0 {
		t.Errorf("First poll should not set Max rates, got In=%v, Out=%v", sw.MaxIn, sw.MaxOut)
	}

	// Second poll: simulate 100KB/s traffic
	now += 1.0
	tables2 := map[string]map[int]uint64{
		OID_HCIN:  {1: 1000 + 102400},
		OID_HCOUT: {1: 2000 + 204800},
	}
	processSamples(sw, tables2, now, delay)
	
	if sw.MaxIn != 100.0 || sw.MaxOut != 200.0 {
		t.Errorf("Second poll MaxIn=%v, MaxOut=%v; want 100.0, 200.0", sw.MaxIn, sw.MaxOut)
	}
	
	if sw.HistIn.Len != 1 || sw.HistIn.Get(0) != 100.0 {
		t.Errorf("HistIn mismatch: len=%d, val=%f", sw.HistIn.Len, sw.HistIn.Get(0))
	}
}
