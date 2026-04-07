package main

import (
	"testing"
)

func TestSampleSerialization(t *testing.T) {
	samples := []Sample{
		{TS: 100.5, Val: 1024.0},
		{TS: 101.5, Val: 2048.0},
		{TS: 102.5, Val: 4096.0},
	}

	b := samplesToBytes(samples)
	if len(b) == 0 {
		t.Fatal("samplesToBytes returned empty slice")
	}

	decoded := bytesToSamples(b)
	if len(decoded) != len(samples) {
		t.Fatalf("decoded len %d, want %d", len(decoded), len(samples))
	}

	for i := range samples {
		if decoded[i].TS != samples[i].TS || decoded[i].Val != samples[i].Val {
			t.Errorf("sample %d mismatch: got %+v, want %+v", i, decoded[i], samples[i])
		}
	}
}

func TestSampleSerializationEmpty(t *testing.T) {
	b := samplesToBytes(nil)
	if b != nil {
		t.Errorf("expected nil bytes for nil slice, got %v", b)
	}

	s := bytesToSamples(nil)
	if s != nil {
		t.Errorf("expected nil samples for nil bytes, got %v", s)
	}
}

func TestSampleSerializationEmptySlice(t *testing.T) {
	b := samplesToBytes([]Sample{})
	if b != nil {
		t.Errorf("expected nil bytes for empty slice, got %v", b)
	}
}

func TestSampleSerializationEmptyBytes(t *testing.T) {
	s := bytesToSamples([]byte{})
	if s != nil {
		t.Errorf("expected nil samples for empty bytes, got %v", s)
	}
}
