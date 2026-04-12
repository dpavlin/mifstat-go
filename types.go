package main

import (
	"sync"
	"time"
)

var MAX_HIST_SEC = 21600.0 // 6 hours default

// Float64Ring is a pre-allocated ring buffer for timestamps.
type Float64Ring struct {
	Data []float64
	Head int
	Len  int
}

func NewFloat64Ring(n int) Float64Ring {
	return Float64Ring{Data: make([]float64, n)}
}

func (r *Float64Ring) Push(v float64) {
	if len(r.Data) == 0 { return }
	r.Data[r.Head] = v
	r.Head = (r.Head + 1) % len(r.Data)
	if r.Len < len(r.Data) {
		r.Len++
	}
}

// Get returns the i-th element from the logical start (oldest).
func (r *Float64Ring) Get(i int) float64 {
	if i < 0 || i >= r.Len { return 0 }
	idx := (r.Head - r.Len + i)
	for idx < 0 { idx += len(r.Data) }
	return r.Data[idx % len(r.Data)]
}

func (r *Float64Ring) GetAll() []float64 {
	res := make([]float64, r.Len)
	for i := 0; i < r.Len; i++ {
		res[i] = r.Get(i)
	}
	return res
}

// Float32Ring is a pre-allocated ring buffer for traffic values.
type Float32Ring struct {
	Data []float32
	Head int
	Len  int
}

func NewFloat32Ring(n int) Float32Ring {
	return Float32Ring{Data: make([]float32, n)}
}

func (r *Float32Ring) Push(v float32) {
	if len(r.Data) == 0 { return }
	r.Data[r.Head] = v
	r.Head = (r.Head + 1) % len(r.Data)
	if r.Len < len(r.Data) {
		r.Len++
	}
}

func (r *Float32Ring) Get(i int) float32 {
	if i < 0 || i >= r.Len { return 0 }
	idx := (r.Head - r.Len + i)
	for idx < 0 { idx += len(r.Data) }
	return r.Data[idx % len(r.Data)]
}

func (r *Float32Ring) GetAll() []float32 {
	res := make([]float32, r.Len)
	for i := 0; i < r.Len; i++ {
		res[i] = r.Get(i)
	}
	return res
}

type PortHistory struct {
	In  Float32Ring
	Out Float32Ring
}

type PortRate struct {
	In, Out       float64
	EmaIn, EmaOut float64
	MaxIn, MaxOut float64
}

type SwitchData struct {
	mu           sync.RWMutex
	Name, IP     string
	Status       string
	In, Out      float64
	EmaIn, EmaOut float64
	MaxIn, MaxOut float64
	
	Timestamps   Float64Ring
	HistIn       Float32Ring
	HistOut      Float32Ring
	LatHist      Float32Ring
	
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

// SaveState holds the deserialized history for migration or loading.
// We keep the map of slices for easier handling in some parts of the code
// but will populate it from rings.
type SaveState struct {
	Timestamps map[string][]float64
	HistIn     map[string][]float32
	HistOut    map[string][]float32
	LatHist    map[string][]float32
	PortHist   map[string]map[string]struct{ In, Out []float32 }
}

type DisplayItem struct {
	IP, Name, SwName, Port, Status string
	In, Out                        float64
	EmaIn, EmaOut                  float64
	MaxIn, MaxOut                  float64
	
	Timestamps                     []float64
	Hist                           []float32
	LatHist                        []float32
	
	SampleInterval                 float64
	LastPollMs                     int64
	SlowMs                         int64
	Detail                         bool
}
