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

	saveState([]*SwitchData{s1}, statePath)
	loaded := loadState(statePath)

	if !reflect.DeepEqual(loaded.HistIn["10.0.0.1"], []float32{10}) {
		t.Errorf("HistIn mismatch: got %v", loaded.HistIn["10.0.0.1"])
	}
}
