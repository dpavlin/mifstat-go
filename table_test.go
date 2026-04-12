package main

import (
	"testing"
)

func TestTableLayout(t *testing.T) {
	headers := []string{"IP", "Name", "IN"}
	// -1 for left align, 1 for right align
	alignments := []int{-1, -1, 1}
	
	rows := [][]string{
		{"10.0.0.1", "sw1", "100.0K"},
		{"10.0.0.123", "core", "1.2M"},
	}

	layout := NewTableLayout(headers, rows, alignments, 1) // 1 is spacing between columns

	expectedHeader := "IP         Name     IN"
	if got := layout.FormatHeader(headers); got != expectedHeader {
		t.Errorf("FormatHeader() = %q; want %q", got, expectedHeader)
	}

	expectedRow0 := "10.0.0.1   sw1  100.0K"
	if got := layout.FormatRow(rows[0]); got != expectedRow0 {
		t.Errorf("FormatRow(0) = %q; want %q", got, expectedRow0)
	}

	expectedRow1 := "10.0.0.123 core   1.2M"
	if got := layout.FormatRow(rows[1]); got != expectedRow1 {
		t.Errorf("FormatRow(1) = %q; want %q", got, expectedRow1)
	}
}
