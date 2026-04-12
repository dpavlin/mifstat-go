package main

import (
	"bufio"
	"encoding/binary"
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

func loadState(path string, states []*SwitchData) {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Printf("loadState: cannot open %s: %v", path, err)
		}
		return
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<20)
	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return
	}

	byIP := make(map[string]*SwitchData)
	for _, sw := range states {
		byIP[sw.IP] = sw
	}

	if string(magic[:]) == "MIFSTAT1" {
		loadStateBinV1(r, byIP)
		return
	}
	if string(magic[:]) == "MIFSTAT2" {
		loadStateBinV2(r, byIP)
		return
	}
	if string(magic[:]) != "MIFSTAT3" {
		return
	}

	nSw, err := readU32(r)
	if err != nil {
		return
	}

	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil { return }
		
		ts, err := readF64Slice(r)
		if err != nil { return }
		in, err := readF32Slice(r)
		if err != nil { return }
		out, err := readF32Slice(r)
		if err != nil { return }
		lat, err := readF32Slice(r)
		if err != nil { return }
		
		sw, ok := byIP[ip]
		if ok {
			for j, t := range ts {
				sw.Timestamps.Push(t)
				if j < len(in) { sw.HistIn.Push(in[j]) }
				if j < len(out) { sw.HistOut.Push(out[j]) }
				if j < len(lat) { sw.LatHist.Push(lat[j]) }
			}
		}

		nPort, err := readU32(r)
		if err != nil { return }
		for j := uint32(0); j < nPort; j++ {
			pname, err := readStr(r)
			if err != nil { return }
			pIn, err := readF32Slice(r)
			if err != nil { return }
			pOut, err := readF32Slice(r)
			if err != nil { return }
			
			if ok {
				if sw.PortHist[pname] == nil {
					sw.PortHist[pname] = &PortHistory{
						In: NewFloat32Ring(len(sw.Timestamps.Data)),
						Out: NewFloat32Ring(len(sw.Timestamps.Data)),
					}
				}
				ph := sw.PortHist[pname]
				for _, v := range pIn { ph.In.Push(v) }
				for _, v := range pOut { ph.Out.Push(v) }
			}
		}
	}
}

func loadStateBinV1(r *bufio.Reader, byIP map[string]*SwitchData) {
	nSw, err := readU32(r)
	if err != nil { return }
	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil { return }
		
		readV1 := func() ([]float64, []float32, error) {
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

		ts, in, _ := readV1()
		_, out, _ := readV1()
		
		sw, ok := byIP[ip]
		if ok {
			for j, t := range ts {
				sw.Timestamps.Push(t)
				if j < len(in) { sw.HistIn.Push(in[j]) }
				if j < len(out) { sw.HistOut.Push(out[j]) }
			}
		}
		
		nPort, _ := readU32(r)
		for j := uint32(0); j < nPort; j++ {
			pname, _ := readStr(r)
			_, pIn, _ := readV1()
			_, pOut, _ := readV1()
			if ok {
				if sw.PortHist[pname] == nil {
					sw.PortHist[pname] = &PortHistory{
						In: NewFloat32Ring(len(sw.Timestamps.Data)),
						Out: NewFloat32Ring(len(sw.Timestamps.Data)),
					}
				}
				ph := sw.PortHist[pname]
				for _, v := range pIn { ph.In.Push(v) }
				for _, v := range pOut { ph.Out.Push(v) }
			}
		}
	}
}

func loadStateBinV2(r *bufio.Reader, byIP map[string]*SwitchData) {
	nSw, err := readU32(r)
	if err != nil { return }
	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil { return }
		
		ts, _ := readF64Slice(r)
		in, _ := readF64Slice(r)
		out, _ := readF64Slice(r)
		lat, _ := readF64Slice(r)
		
		sw, ok := byIP[ip]
		if ok {
			for j, t := range ts {
				sw.Timestamps.Push(t)
				if j < len(in) { sw.HistIn.Push(float32(in[j])) }
				if j < len(out) { sw.HistOut.Push(float32(out[j])) }
				if j < len(lat) { sw.LatHist.Push(float32(lat[j])) }
			}
		}

		nPort, _ := readU32(r)
		for j := uint32(0); j < nPort; j++ {
			pname, _ := readStr(r)
			pLen, _ := readU32(r)
			ft, _ := readF64Slice(r)
			_ = ft; _ = pLen
			pIn, _ := readF64Slice(r)
			pOut, _ := readF64Slice(r)
			if ok {
				if sw.PortHist[pname] == nil {
					sw.PortHist[pname] = &PortHistory{
						In: NewFloat32Ring(len(sw.Timestamps.Data)),
						Out: NewFloat32Ring(len(sw.Timestamps.Data)),
					}
				}
				ph := sw.PortHist[pname]
				for _, v := range pIn { ph.In.Push(float32(v)) }
				for _, v := range pOut { ph.Out.Push(float32(v)) }
			}
		}
	}
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
