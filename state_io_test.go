package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestStateIOHelpers(t *testing.T) {
	buf := new(bytes.Buffer)
	
	// Test normal string
	writeStr(buf, "hello")
	// Test empty string
	writeStr(buf, "")
	// Test too long string (limit 255)
	longStr := strings.Repeat("a", 300)
	writeStr(buf, longStr)

	res := bytes.NewReader(buf.Bytes())
	
	s1, _ := readStr(res)
	if s1 != "hello" { t.Errorf("expected hello, got %q", s1) }
	
	s2, _ := readStr(res)
	if s2 != "" { t.Errorf("expected empty, got %q", s2) }
	
	s3, _ := readStr(res)
	if len(s3) != 255 { t.Errorf("expected length 255 for long string, got %d", len(s3)) }
}

func TestReadU32Error(t *testing.T) {
	// Test unexpected EOF in readU32
	buf := bytes.NewReader([]byte{1, 2, 3})
	_, err := readU32(buf)
	if err == nil {
		t.Error("expected error for short read in readU32")
	}
}
