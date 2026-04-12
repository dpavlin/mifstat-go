package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unsafe"
)

var stateMagic = [8]byte{'M', 'I', 'F', 'S', 'T', 'A', 'T', '2'}

func writeF64Slice(w io.Writer, s []float64) {
	writeU32(w, uint32(len(s)))
	if len(s) > 0 {
		w.Write(unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*8))
	}
}

func readF64Slice(r io.Reader) ([]float64, error) {
	n, err := readU32(r)
	if err != nil || n == 0 {
		return nil, err
	}
	s := make([]float64, n)
	_, err = io.ReadFull(r, unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), int(n)*8))
	return s, err
}

func toF64(s []Sample) ([]float64, []float64) {
	ts := make([]float64, len(s))
	val := make([]float64, len(s))
	for i, x := range s {
		ts[i], val[i] = x.TS, x.Val
	}
	return ts, val
}

func toSamples(ts, val []float64) []Sample {
	n := len(ts)
	if len(val) < n {
		n = len(val)
	}
	if n == 0 {
		return nil
	}
	s := make([]Sample, n)
	// We use the LAST n timestamps from ts to align with val
	// (assuming val are the most recent samples).
	tsOffset := len(ts) - n
	for i := 0; i < n; i++ {
		s[i] = Sample{TS: ts[tsOffset+i], Val: val[i]}
	}
	return s
}

func saveState(states []*SwitchData, path string) {
	f, err := os.Create(path)
	if err != nil {
		logger.Printf("saveState: cannot create %s: %v", path, err)
		return
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)

	w.Write(stateMagic[:])

	// Count switches that actually have history.
	var sws []*SwitchData
	for _, sw := range states {
		sw.mu.RLock()
		if len(sw.HistIn) > 0 {
			sws = append(sws, sw)
		}
		sw.mu.RUnlock()
	}
	writeU32(w, uint32(len(sws)))

	for _, sw := range sws {
		sw.mu.RLock()
		writeStr(w, sw.IP)
		
		ts, in := toF64(sw.HistIn)
		_, out := toF64(sw.HistOut)
		_, lat := toF64(sw.LatHist)
		
		writeF64Slice(w, ts)
		writeF64Slice(w, in)
		writeF64Slice(w, out)
		writeF64Slice(w, lat)

		// Count ports with data.
		nport := 0
		for _, ph := range sw.PortHist {
			if len(ph.In) > 0 {
				nport++
			}
		}
		writeU32(w, uint32(nport))
		for pname, ph := range sw.PortHist {
			if len(ph.In) == 0 {
				continue
			}
			writeStr(w, pname)
			writeU32(w, uint32(len(ph.In)))
			firstTS := 0.0
			if len(ph.In) > 0 {
				firstTS = ph.In[0].TS
			}
			writeF64Slice(w, []float64{firstTS})
			
			_, pIn := toF64(ph.In)
			_, pOut := toF64(ph.Out)
			writeF64Slice(w, pIn)
			writeF64Slice(w, pOut)
		}
		sw.mu.RUnlock()
	}
	
	if err := w.Flush(); err != nil {
		logger.Printf("saveState: flush error: %v", err)
	}
}

func loadStateBinV1(r *bufio.Reader) (SaveState, error) {
	var s SaveState
	nSw, err := readU32(r)
	if err != nil {
		return s, err
	}
	s.HistIn = make(map[string][]Sample, nSw)
	s.HistOut = make(map[string][]Sample, nSw)
	s.PortHist = make(map[string]map[string]*PortHistory, nSw)
	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil { return s, err }
		hIn, err := readSamples(r)
		if err != nil { return s, err }
		hOut, err := readSamples(r)
		if err != nil { return s, err }
		s.HistIn[ip] = hIn
		s.HistOut[ip] = hOut
		nPort, err := readU32(r)
		if err != nil { return s, err }
		ph := make(map[string]*PortHistory, nPort)
		for j := uint32(0); j < nPort; j++ {
			pname, err := readStr(r)
			if err != nil { return s, err }
			pIn, err := readSamples(r)
			if err != nil { return s, err }
			pOut, err := readSamples(r)
			if err != nil { return s, err }
			ph[pname] = &PortHistory{In: pIn, Out: pOut}
		}
		s.PortHist[ip] = ph
	}
	return s, nil
}

func loadStateBin(path string) (SaveState, error) {
	var s SaveState
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Printf("loadStateBin: cannot open %s: %v", path, err)
		}
		return s, err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<20)
	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return s, err
	}

	if string(magic[:]) == "MIFSTAT1" {
		return loadStateBinV1(r)
	}
	if string(magic[:]) != "MIFSTAT2" {
		return s, fmt.Errorf("bad magic: %q", string(magic[:]))
	}

	nSw, err := readU32(r)
	if err != nil {
		return s, err
	}

	s.HistIn = make(map[string][]Sample, nSw)
	s.HistOut = make(map[string][]Sample, nSw)
	s.LatHist = make(map[string][]Sample, nSw)
	s.PortHist = make(map[string]map[string]*PortHistory, nSw)

	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil { return s, err }
		
		ts, err := readF64Slice(r)
		if err != nil { return s, err }
		in, err := readF64Slice(r)
		if err != nil { return s, err }
		out, err := readF64Slice(r)
		if err != nil { return s, err }
		lat, err := readF64Slice(r)
		if err != nil { return s, err }
		
		s.HistIn[ip] = toSamples(ts, in)
		s.HistOut[ip] = toSamples(ts, out)
		s.LatHist[ip] = toSamples(ts, lat)

		nPort, err := readU32(r)
		if err != nil { return s, err }
		ph := make(map[string]*PortHistory, nPort)
		for j := uint32(0); j < nPort; j++ {
			pname, err := readStr(r)
			if err != nil { return s, err }
			pLen, err := readU32(r)
			if err != nil { return s, err }
			
			ft, err := readF64Slice(r)
			if err != nil { return s, err }
			firstTS := 0.0
			if len(ft) > 0 { firstTS = ft[0] }

			pIn, err := readF64Slice(r)
			if err != nil { return s, err }
			pOut, err := readF64Slice(r)
			if err != nil { return s, err }
			
			resIn := make([]Sample, pLen)
			resOut := make([]Sample, pLen)
			
			offset := -1
			for idx, t := range ts {
				if t == firstTS {
					offset = idx
					break
				}
			}
			
			for k := 0; k < int(pLen); k++ {
				curTS := firstTS + float64(k)
				if offset != -1 && offset+k < len(ts) {
					curTS = ts[offset+k]
				}
				resIn[k] = Sample{TS: curTS, Val: pIn[k]}
				resOut[k] = Sample{TS: curTS, Val: pOut[k]}
			}
			
			ph[pname] = &PortHistory{In: resIn, Out: resOut}
		}
		s.PortHist[ip] = ph
	}
	return s, nil
}

func loadState(path string) SaveState {
	s, _ := loadStateBin(path)
	return s
}

func samplesToBytes(s []Sample) []byte {
	if len(s) == 0 {
		return nil
	}
	sz := int(unsafe.Sizeof(Sample{}))
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*sz)
}

func bytesToSamples(b []byte) []Sample {
	sz := int(unsafe.Sizeof(Sample{}))
	n := len(b) / sz
	if n == 0 {
		return nil
	}
	s := make([]Sample, n)
	copy(unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(b)), b)
	return s
}

func pu32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

func writeU32(w io.Writer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.Write(b[:])
}

func writeStr(w io.Writer, s string) {
	if len(s) > 255 {
		s = s[:255]
	}
	w.Write([]byte{byte(len(s))})
	io.WriteString(w, s)
}

func writeSamples(w io.Writer, s []Sample) {
	writeU32(w, uint32(len(s)))
	if len(s) > 0 {
		w.Write(samplesToBytes(s))
	}
}

func readFull(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

func readU32(r io.Reader) (uint32, error) {
	b, err := readFull(r, 4)
	if err != nil {
		return 0, err
	}
	return pu32(b), nil
}

func readStr(r io.Reader) (string, error) {
	b1 := []byte{0}
	if _, err := io.ReadFull(r, b1); err != nil {
		return "", err
	}
	if b1[0] == 0 {
		return "", nil
	}
	b, err := readFull(r, int(b1[0]))
	return string(b), err
}

func readSamples(r io.Reader) ([]Sample, error) {
	n, err := readU32(r)
	if err != nil || n == 0 {
		return nil, err
	}
	s := make([]Sample, n)
	sz := int(unsafe.Sizeof(Sample{}))
	_, err = io.ReadFull(r, unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), int(n)*sz))
	return s, err
}
