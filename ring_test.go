package main

import (
	"testing"
)

func TestFloat64RingExhaustive(t *testing.T) {
	r := NewFloat64Ring(5)
	
	// Test empty Get
	if v := r.Get(0); v != 0 {
		t.Errorf("Get(0) on empty ring should be 0, got %f", v)
	}
	if v := r.Get(-1); v != 0 {
		t.Errorf("Get(-1) should be 0, got %f", v)
	}
	if v := r.Get(10); v != 0 {
		t.Errorf("Get(10) should be 0, got %f", v)
	}

	// Fill partially
	r.Push(1.0)
	r.Push(2.0)
	if r.Len != 2 { t.Fatal("len mismatch") }
	if r.Get(0) != 1.0 || r.Get(1) != 2.0 {
		t.Errorf("partial Get mismatch: [0]=%f [1]=%f", r.Get(0), r.Get(1))
	}

	// Fill completely
	r.Push(3.0)
	r.Push(4.0)
	r.Push(5.0)
	if r.Len != 5 { t.Fatal("len mismatch") }
	
	// Wrap once
	r.Push(6.0) // replaces 1.0, Head is now 1
	if r.Len != 5 { t.Fatal("len mismatch after wrap") }
	if r.Get(0) != 2.0 {
		t.Errorf("oldest after wrap should be 2.0, got %f", r.Get(0))
	}
	if r.Get(4) != 6.0 {
		t.Errorf("newest after wrap should be 6.0, got %f", r.Get(4))
	}

	// Wrap multiple times
	for i := 0; i < 10; i++ {
		r.Push(float64(10 + i))
	}
	if r.Get(0) != 15.0 || r.Get(4) != 19.0 {
		t.Errorf("multi wrap Get mismatch: [0]=%f [4]=%f", r.Get(0), r.Get(4))
	}
}

func TestFloat32RingExhaustive(t *testing.T) {
	r := NewFloat32Ring(3)
	// Test boundary Get
	if r.Get(0) != 0 { t.Error("empty Get error") }
	
	r.Push(10)
	r.Push(20)
	r.Push(30)
	r.Push(40) // wrap
	
	if r.Get(0) != 20 || r.Get(2) != 40 {
		t.Errorf("float32 wrap mismatch: [0]=%f [2]=%f", r.Get(0), r.Get(2))
	}
}
