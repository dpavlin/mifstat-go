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

	states := []*SwitchData{
		{
			IP:     "10.0.0.1",
			HistIn: []Sample{{TS: 100, Val: 10}, {TS: 101, Val: 20}},
			HistOut: []Sample{{TS: 100, Val: 5}, {TS: 101, Val: 15}},
			PortHist: map[string]*PortHistory{
				"p1": {
					In:  []Sample{{TS: 100, Val: 1}},
					Out: []Sample{{TS: 100, Val: 2}},
				},
			},
		},
	}

	saveState(states, statePath)

	loaded := loadState(statePath)

	if !reflect.DeepEqual(loaded.HistIn["10.0.0.1"], states[0].HistIn) {
		t.Errorf("HistIn mismatch: got %+v, want %+v", loaded.HistIn["10.0.0.1"], states[0].HistIn)
	}
	if !reflect.DeepEqual(loaded.HistOut["10.0.0.1"], states[0].HistOut) {
		t.Errorf("HistOut mismatch: got %+v, want %+v", loaded.HistOut["10.0.0.1"], states[0].HistOut)
	}
	if !reflect.DeepEqual(loaded.PortHist["10.0.0.1"]["p1"], states[0].PortHist["p1"]) {
		t.Errorf("PortHist mismatch: got %+v, want %+v", loaded.PortHist["10.0.0.1"]["p1"], states[0].PortHist["p1"])
	}
}
