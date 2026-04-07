package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unsafe"
)

var stateMagic = [8]byte{'M', 'I', 'F', 'S', 'T', 'A', 'T', '1'}

func saveState(states []*SwitchData, path string) {
	f, err := os.Create(path)
	if err != nil {
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
		writeSamples(w, sw.HistIn)
		writeSamples(w, sw.HistOut)
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
			writeSamples(w, ph.In)
			writeSamples(w, ph.Out)
		}
		sw.mu.RUnlock()
	}
	w.Flush()
}

func loadStateBin(path string) (SaveState, error) {
	var s SaveState
	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<20)
	var magic [8]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return s, err
	}
	if magic != stateMagic {
		return s, fmt.Errorf("bad magic")
	}

	nSw, err := readU32(r)
	if err != nil {
		return s, err
	}

	s.HistIn = make(map[string][]Sample, nSw)
	s.HistOut = make(map[string][]Sample, nSw)
	s.PortHist = make(map[string]map[string]*PortHistory, nSw)

	for i := uint32(0); i < nSw; i++ {
		ip, err := readStr(r)
		if err != nil {
			return s, err
		}
		histIn, err := readSamples(r)
		if err != nil {
			return s, err
		}
		histOut, err := readSamples(r)
		if err != nil {
			return s, err
		}
		s.HistIn[ip] = histIn
		s.HistOut[ip] = histOut
		nPort, err := readU32(r)
		if err != nil {
			return s, err
		}
		ph := make(map[string]*PortHistory, nPort)
		for j := uint32(0); j < nPort; j++ {
			pname, err := readStr(r)
			if err != nil {
				return s, err
			}
			pIn, err := readSamples(r)
			if err != nil {
				return s, err
			}
			pOut, err := readSamples(r)
			if err != nil {
				return s, err
			}
			ph[pname] = &PortHistory{In: pIn, Out: pOut}
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

func writeU32(w *bufio.Writer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.Write(b[:])
}

func writeStr(w *bufio.Writer, s string) {
	if len(s) > 255 {
		s = s[:255]
	}
	w.WriteByte(byte(len(s)))
	w.WriteString(s)
}

func writeSamples(w *bufio.Writer, s []Sample) {
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
	sz := int(unsafe.Sizeof(Sample{}))
	b, err := readFull(r, int(n)*sz)
	if err != nil {
		return nil, err
	}
	return bytesToSamples(b), nil
}
