package main

import (
	"testing"
)

func TestTrafficMaxTracking(t *testing.T) {
	sw := &SwitchData{
		Rates: make(map[string]*PortRate),
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
	// (102400 bytes diff / 1024 / 1.0s = 100K)
	now += 1.0
	tables2 := map[string]map[int]uint64{
		OID_HCIN:  {1: 1000 + 102400},
		OID_HCOUT: {1: 2000 + 204800},
	}
	processSamples(sw, tables2, now, delay)
	
	if sw.MaxIn != 100.0 || sw.MaxOut != 200.0 {
		t.Errorf("Second poll MaxIn=%v, MaxOut=%v; want 100.0, 200.0", sw.MaxIn, sw.MaxOut)
	}
	
	// Third poll: lower traffic, Max should stay same
	now += 1.0
	tables3 := map[string]map[int]uint64{
		OID_HCIN:  {1: 1000 + 102400 + 51200},
		OID_HCOUT: {1: 2000 + 204800 + 102400},
	}
	processSamples(sw, tables3, now, delay)
	
	if sw.MaxIn != 100.0 || sw.MaxOut != 200.0 {
		t.Errorf("Third poll (lower traffic) MaxIn=%v, MaxOut=%v; want 100.0, 200.0", sw.MaxIn, sw.MaxOut)
	}
	
	// Fourth poll: higher traffic, Max should increase
	now += 1.0
	tables4 := map[string]map[int]uint64{
		OID_HCIN:  {1: 1000 + 102400 + 51200 + 307200},
		OID_HCOUT: {1: 2000 + 204800 + 102400 + 409600},
	}
	processSamples(sw, tables4, now, delay)
	
	if sw.MaxIn != 300.0 || sw.MaxOut != 400.0 {
		t.Errorf("Fourth poll (higher traffic) MaxIn=%v, MaxOut=%v; want 300.0, 400.0", sw.MaxIn, sw.MaxOut)
	}
}
