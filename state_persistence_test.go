package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveLoadState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mifstat-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	statePath := filepath.Join(tmpDir, "state.bin")

	s1 := &SwitchData{
		IP:     "10.0.0.1",
		Timestamps: NewFloat64Ring(10),
		HistIn:     NewFloat32Ring(10),
		HistOut:    NewFloat32Ring(10),
		LatHist:    NewFloat32Ring(10),
		PortHist:   make(map[string]*PortHistory),
	}
	s1.Timestamps.Push(100)
	s1.HistIn.Push(10)
	s1.HistOut.Push(5)
	s1.LatHist.Push(50)
	
	s1.Timestamps.Push(101)
	s1.HistIn.Push(20)
	s1.HistOut.Push(15)
	s1.LatHist.Push(60)

	ph := &PortHistory{
		In:  NewFloat32Ring(10),
		Out: NewFloat32Ring(10),
	}
	ph.In.Push(1)
	ph.Out.Push(2)
	s1.PortHist["p1"] = ph

	saveState([]*SwitchData{s1}, statePath)

	sLoaded := &SwitchData{
		IP:         "10.0.0.1",
		Timestamps: NewFloat64Ring(10),
		HistIn:     NewFloat32Ring(10),
		HistOut:    NewFloat32Ring(10),
		LatHist:    NewFloat32Ring(10),
		PortHist:   make(map[string]*PortHistory),
	}
	loadState(statePath, []*SwitchData{sLoaded})

	if !reflect.DeepEqual(sLoaded.HistIn.GetAll(), []float32{10, 20}) {
		t.Errorf("HistIn mismatch: got %v", sLoaded.HistIn.GetAll())
	}
	if !reflect.DeepEqual(sLoaded.HistOut.GetAll(), []float32{5, 15}) {
		t.Errorf("HistOut mismatch")
	}
	if !reflect.DeepEqual(sLoaded.Timestamps.GetAll(), []float64{100, 101}) {
		t.Errorf("Timestamps mismatch: got %v", sLoaded.Timestamps.GetAll())
	}
	// Note: in MIFSTAT3, port timestamps are reconstruction of switch timestamps.
	// Since we had 2 switch samples but only 1 port sample, it should align with the second (latest) switch TS.
	phLoaded, ok := sLoaded.PortHist["p1"]
	if !ok || phLoaded.In.Len != 1 || phLoaded.In.Get(0) != 1 {
		t.Errorf("PortHist In mismatch")
	}
}

func TestLoadStateMissing(t *testing.T) {
	// Loading non-existent file should just return without crash
	s := &SwitchData{IP: "1.1.1.1"}
	loadState("/non/existent/path/mifstat.bin", []*SwitchData{s})
	if s.Timestamps.Len != 0 {
		t.Errorf("expected empty ring for missing file")
	}
}
