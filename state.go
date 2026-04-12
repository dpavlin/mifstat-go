package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unsafe"
)

var stateMagic = [8]byte{'M', 'I', 'F', 'S', 'T', 'A', 'T', '3'}

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

func writeF32Slice(w io.Writer, s []float32) {
	writeU32(w, uint32(len(s)))
	if len(s) > 0 {
		w.Write(unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*4))
	}
}

func readF32Slice(r io.Reader) ([]float32, error) {
	n, err := readU32(r)
	if err != nil || n == 0 {
		return nil, err
	}
	s := make([]float32, n)
	_, err = io.ReadFull(r, unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), int(n)*4))
	return s, err
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
		if sw.Timestamps.Len > 0 {
			sws = append(sws, sw)
		}
		sw.mu.RUnlock()
	}
	writeU32(w, uint32(len(sws)))

	for _, sw := range sws {
		sw.mu.RLock()
		writeStr(w, sw. IP)
		
		writeF64Slice(w, sw.Timestamps.GetAll())
		writeF32Slice(w, sw.HistIn.GetAll())
		writeF32Slice(w, sw.HistOut.GetAll())
		writeF32Slice(w, sw.LatHist.GetAll())

		// Count ports with data.
		nport := 0
		for _, ph := range sw.PortHist {
			if ph.In.Len > 0 {
				nport++
			}
		}
		writeU32(w, uint32(nport))
		for pname, ph := range sw.PortHist {
			if ph.In.Len == 0 {
				continue
			}
			writeStr(w, pname)
			writeF32Slice(w, ph.In.GetAll())
			writeF32Slice(w, ph.Out.GetAll())
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
	s.HistIn = make(map[string][]float32, nSw)
	s.HistOut = make(map[string][]float32, nSw)
	s.LatHist = make(map[string][]float32, nSw)
	s.Timestamps = make(map[string][]float64, nSw)
	s.PortHist = make(map[string]map[string]struct{ In, Out []float32 }, nSw)

	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil { return s, err }
		
		// V1 used []Sample {TS, Val float64}
		readV1Samples := func() ([]float64, []float32, error) {
			n, err := readU32(r)
			if err != nil { return nil, nil, err }
			ts := make([]float64, n)
			val := make([]float32, n)
			for j := uint32(0); j < n; j++ {
				var t, v float64
				binary.Read(r, binary.LittleEndian, &t)
				binary.Read(r, binary.LittleEndian, &v)
				ts[j] = t
				val[j] = float32(v)
			}
			return ts, val, nil
		}

		ts, in, err := readV1Samples()
		if err != nil { return s, err }
		_, out, err := readV1Samples()
		if err != nil { return s, err }
		
		s.Timestamps[ip] = ts
		s.HistIn[ip] = in
		s.HistOut[ip] = out
		
		nPort, err := readU32(r)
		if err != nil { return s, err }
		phMap := make(map[string]struct{ In, Out []float32 }, nPort)
		for j := uint32(0); j < nPort; j++ {
			pname, err := readStr(r)
			if err != nil { return s, err }
			_, pIn, err := readV1Samples()
			if err != nil { return s, err }
			_, pOut, err := readV1Samples()
			if err != nil { return s, err }
			phMap[pname] = struct{ In, Out []float32 }{pIn, pOut}
		}
		s.PortHist[ip] = phMap
	}
	return s, nil
}

func loadStateBinV2(r *bufio.Reader) (SaveState, error) {
	var s SaveState
	nSw, err := readU32(r)
	if err != nil {
		return s, err
	}
	s.HistIn = make(map[string][]float32, nSw)
	s.HistOut = make(map[string][]float32, nSw)
	s.LatHist = make(map[string][]float32, nSw)
	s.Timestamps = make(map[string][]float64, nSw)
	s.PortHist = make(map[string]map[string]struct{ In, Out []float32 }, nSw)

	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil { return s, err }
		
		ts, err := readF64Slice(r)
		if err != nil { return s, err }
		in, err := readF64Slice(r) // V2 was float64
		if err != nil { return s, err }
		out, err := readF64Slice(r)
		if err != nil { return s, err }
		lat, err := readF64Slice(r)
		if err != nil { return s, err }
		
		s.Timestamps[ip] = ts
		s.HistIn[ip] = f64to32(in)
		s.HistOut[ip] = f64to32(out)
		s.LatHist[ip] = f64to32(lat)

		nPort, err := readU32(r)
		if err != nil { return s, err }
		phMap := make(map[string]struct{ In, Out []float32 }, nPort)
		for j := uint32(0); j < nPort; j++ {
			pname, err := readStr(r)
			if err != nil { return s, err }
			pLen, _ := readU32(r)
			ft, _ := readF64Slice(r) // firstTS
			_ = ft
			_ = pLen
			pIn, _ := readF64Slice(r)
			pOut, _ := readF64Slice(r)
			phMap[pname] = struct{ In, Out []float32 }{f64to32(pIn), f64to32(pOut)}
		}
		s.PortHist[ip] = phMap
	}
	return s, nil
}

func f64to32(s []float64) []float32 {
	res := make([]float32, len(s))
	for i, v := range s {
		res[i] = float32(v)
	}
	return res
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
	if string(magic[:]) == "MIFSTAT2" {
		return loadStateBinV2(r)
	}
	if string(magic[:]) != "MIFSTAT3" {
		return s, fmt.Errorf("bad magic: %q", string(magic[:]))
	}

	nSw, err := readU32(r)
	if err != nil {
		return s, err
	}

	s.Timestamps = make(map[string][]float64, nSw)
	s.HistIn = make(map[string][]float32, nSw)
	s.HistOut = make(map[string][]float32, nSw)
	s.LatHist = make(map[string][]float32, nSw)
	s.PortHist = make(map[string]map[string]struct{ In, Out []float32 }, nSw)

	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil { return s, err }
		
		ts, err := readF64Slice(r)
		if err != nil { return s, err }
		in, err := readF32Slice(r)
		if err != nil { return s, err }
		out, err := readF32Slice(r)
		if err != nil { return s, err }
		lat, err := readF32Slice(r)
		if err != nil { return s, err }
		
		s.Timestamps[ip] = ts
		s.HistIn[ip] = in
		s.HistOut[ip] = out
		s.LatHist[ip] = lat

		nPort, err := readU32(r)
		if err != nil { return s, err }
		phMap := make(map[string]struct{ In, Out []float32 }, nPort)
		for j := uint32(0); j < nPort; j++ {
			pname, err := readStr(r)
			if err != nil { return s, err }
			pIn, err := readF32Slice(r)
			if err != nil { return s, err }
			pOut, err := readF32Slice(r)
			if err != nil { return s, err }
			phMap[pname] = struct{ In, Out []float32 }{pIn, pOut}
		}
		s.PortHist[ip] = phMap
	}
	return s, nil
}

func loadState(path string) SaveState {
	s, _ := loadStateBin(path)
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
