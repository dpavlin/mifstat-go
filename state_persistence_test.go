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

	loaded := loadState(statePath)

	if !reflect.DeepEqual(loaded.HistIn["10.0.0.1"], []float32{10, 20}) {
		t.Errorf("HistIn mismatch: got %v", loaded.HistIn["10.0.0.1"])
	}
	if !reflect.DeepEqual(loaded.HistOut["10.0.0.1"], []float32{5, 15}) {
		t.Errorf("HistOut mismatch")
	}
	if !reflect.DeepEqual(loaded.Timestamps["10.0.0.1"], []float64{100, 101}) {
		t.Errorf("Timestamps mismatch: got %v", loaded.Timestamps["10.0.0.1"])
	}
	// Note: in MIFSTAT3, port timestamps are reconstruction of switch timestamps.
	// Since we had 2 switch samples but only 1 port sample, it should align with the second (latest) switch TS.
	pIn := loaded.PortHist["10.0.0.1"]["p1"].In
	if len(pIn) != 1 || pIn[0] != 1 {
		t.Errorf("PortHist In mismatch")
	}
}
