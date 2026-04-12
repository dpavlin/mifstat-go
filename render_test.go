package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestRenderTraffic(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	screen.SetSize(80, 24)

	items := []DisplayItem{
		{
			IP: "10.0.0.1", Name: "sw1", In: 100, Out: 200,
			EmaIn: 100, EmaOut: 200, MaxIn: 1000, MaxOut: 2000,
			Status: "OK",
			Timestamps: []float64{100.0, 101.0},
			Hist: []float32{10.0, 20.0},
		},
	}

	defStyle := tcell.StyleDefault
	revStyle := defStyle.Reverse(true)
	dimStyle := defStyle.Dim(true)
	warnStyle := defStyle.Foreground(tcell.ColorYellow)

	renderTraffic(screen, items, 24, 80, revStyle, warnStyle, defStyle, dimStyle, true)

	// Check if some content was rendered.
	// Headers
	foundIP := false
	foundName := false
	for x := 0; x < 80; x++ {
		c, _, _, _ := screen.GetContent(x, 0)
		if c == 'I' {
			c2, _, _, _ := screen.GetContent(x+1, 0)
			if c2 == 'P' {
				foundIP = true
			}
		}
		if c == 'N' {
			c2, _, _, _ := screen.GetContent(x+1, 0)
			if c2 == 'a' {
				foundName = true
			}
		}
	}

	if !foundIP || !foundName {
		t.Errorf("Header not found: IP=%v, Name=%v", foundIP, foundName)
	}

	// Data row
	foundData := false
	for x := 0; x < 80; x++ {
		c, _, _, _ := screen.GetContent(x, 1)
		if c == '1' {
			c2, _, _, _ := screen.GetContent(x+1, 1)
			if c2 == '0' {
				foundData = true
			}
		}
	}
	if !foundData {
		t.Error("Data row (10.0.0.1) not found")
	}
}

func TestRenderPerf(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	screen.SetSize(100, 24)

	states := []*SwitchData{
		{
			IP: "10.0.0.1", Name: "sw1", Status: "OK",
			PollCount: 10, ErrorCount: 0, PhysPorts: 24, IfaceCount: 26,
			LastPollMs: 50, TotalPollMs: 500, SampleInterval: 1.0,
			MaxRepetitions: 20,
		},
	}

	defStyle := tcell.StyleDefault
	revStyle := defStyle.Reverse(true)
	dimStyle := defStyle.Dim(true)
	warnStyle := defStyle.Foreground(tcell.ColorYellow)

	renderPerf(screen, states, 24, 100, revStyle, warnStyle, defStyle, dimStyle, true)

	// Check headers
	foundAvg := false
	for x := 0; x < 100; x++ {
		c, _, _, _ := screen.GetContent(x, 0)
		if c == 'A' {
			c2, _, _, _ := screen.GetContent(x+1, 0)
			if c2 == 'v' {
				foundAvg = true
			}
		}
	}
	if !foundAvg {
		t.Error("Header 'Avg(ms)' not found in renderPerf")
	}

	// Check data
	foundIP := false
	for x := 0; x < 100; x++ {
		c, _, _, _ := screen.GetContent(x, 1)
		if c == '1' {
			c2, _, _, _ := screen.GetContent(x+1, 1)
			if c2 == '0' {
				foundIP = true
			}
		}
	}
	if !foundIP {
		t.Error("Data row (10.0.0.1) not found in renderPerf")
	}
}
