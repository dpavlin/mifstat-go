package main

import (
	"sync"
	"time"
)

// Sample is a (timestamp, value) data point for history sparklines.
type Sample struct{ TS, Val float64 }

const MAX_HIST_SEC = 21600.0 // 6 hours

type PortHistory struct{ In, Out []Sample }

type PortRate struct {
	In, Out       float64
	EmaIn, EmaOut float64
}

type SwitchData struct {
	mu           sync.RWMutex
	Name, IP     string
	Status       string
	In, Out      float64
	EmaIn, EmaOut float64
	HistIn       []Sample
	HistOut      []Sample
	PortHist     map[string]*PortHistory
	Rates        map[string]*PortRate
	prevCounters map[string][2]uint64
	prevTS       time.Time

	// Performance metrics (not persisted)
	PollCount      uint64
	ErrorCount     uint64
	LastPollMs     int64
	TotalPollMs    int64
	PhysPorts      int     // ethernetCsmacd + LAG interfaces
	IfaceCount     int     // all SNMP interfaces
	SampleInterval float64 // EMA of actual inter-sample time (seconds)
	MaxRepetitions uint32  // SNMP GetBulk max repetitions
}

// SaveState holds only the serialisable history (no mutexes or counters).
type SaveState struct {
	HistIn   map[string][]Sample
	HistOut  map[string][]Sample
	PortHist map[string]map[string]*PortHistory
}

type DisplayItem struct {
	IP, Name, SwName, Port, Status string
	In, Out                        float64
	EmaIn, EmaOut                  float64
	Hist                           []Sample
	SampleInterval                 float64
	LastPollMs                     int64
	Detail                         bool
}
