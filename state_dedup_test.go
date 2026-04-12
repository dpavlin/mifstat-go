package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveLoadStateComplete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mifstat-dedup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	statePath := filepath.Join(tmpDir, "state.bin")

	// Create a switch with some data
	s1 := &SwitchData{
		IP:      "10.0.0.1",
		HistIn:  []Sample{{TS: 100, Val: 10}, {TS: 101, Val: 20}},
		HistOut: []Sample{{TS: 100, Val: 5}, {TS: 101, Val: 15}},
		LatHist: []Sample{{TS: 100, Val: 50}, {TS: 101, Val: 60}},
		PortHist: map[string]*PortHistory{
			"p1": {
				In:  []Sample{{TS: 100, Val: 1}, {TS: 101, Val: 2}},
				Out: []Sample{{TS: 100, Val: 3}, {TS: 101, Val: 4}},
			},
		},
	}

	saveState([]*SwitchData{s1}, statePath)
	loaded := loadState(statePath)

	// Check if LatHist is preserved (this will fail initially)
	if !reflect.DeepEqual(loaded.LatHist["10.0.0.1"], s1.LatHist) {
		t.Errorf("LatHist not preserved: got %+v, want %+v", loaded.LatHist["10.0.0.1"], s1.LatHist)
	}

	// Verify other data
	if !reflect.DeepEqual(loaded.HistIn["10.0.0.1"], s1.HistIn) {
		t.Errorf("HistIn mismatch")
	}
	if !reflect.DeepEqual(loaded.PortHist["10.0.0.1"]["p1"], s1.PortHist["p1"]) {
		t.Errorf("PortHist mismatch")
	}
}

func TestStateFileEfficiency(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mifstat-efficiency-*")
	defer os.RemoveAll(tmpDir)
	statePath := filepath.Join(tmpDir, "state.bin")

	// 1 switch, 50 ports, 1000 samples
	numPorts := 50
	numSamples := 1000
	sw := &SwitchData{
		IP:       "10.0.0.1",
		PortHist: make(map[string]*PortHistory),
	}
	for i := 0; i < numSamples; i++ {
		ts := float64(1000 + i)
		sw.HistIn = append(sw.HistIn, Sample{TS: ts, Val: float64(i)})
		sw.HistOut = append(sw.HistOut, Sample{TS: ts, Val: float64(i * 2)})
	}
	for p := 0; p < numPorts; p++ {
		pname := filepath.Join("p", string(rune(p)))
		ph := &PortHistory{}
		for i := 0; i < numSamples; i++ {
			ts := float64(1000 + i)
			ph.In = append(ph.In, Sample{TS: ts, Val: float64(i)})
			ph.Out = append(ph.Out, Sample{TS: ts, Val: float64(i)})
		}
		sw.PortHist[pname] = ph
	}

	saveState([]*SwitchData{sw}, statePath)
	info, _ := os.Stat(statePath)
	
	// Naive size: (2 + 2*50) * 1000 * 16 bytes ~= 1.6 MB
	// Dedup size: (1 (ts) + 2 + 2*50) * 1000 * 8 bytes ~= 0.8 MB
	
	t.Logf("State file size with %d ports and %d samples: %d bytes", numPorts, numSamples, info.Size())
	
	// We expect the size to be closer to 0.8MB than 1.6MB once optimized.
	// For now, we just record the baseline.
}
