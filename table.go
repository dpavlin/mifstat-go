package main

import (
	"fmt"
	"strings"
)

type TableLayout struct {
	widths     []int
	alignments []int // -1 for left, 1 for right
	spacing    int
}

func NewTableLayout(headers []string, rows [][]string, alignments []int, spacing int) *TableLayout {
	n := len(headers)
	widths := make([]int, n)
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < n && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	return &TableLayout{
		widths:     widths,
		alignments: alignments,
		spacing:    spacing,
	}
}

func (t *TableLayout) FormatHeader(headers []string) string {
	return t.FormatRow(headers)
}

func (t *TableLayout) FormatRow(row []string) string {
	var sb strings.Builder
	space := strings.Repeat(" ", t.spacing)
	for i, cell := range row {
		if i >= len(t.widths) {
			break
		}
		if i > 0 {
			sb.WriteString(space)
		}
		
		width := t.widths[i]
		align := -1 // default left
		if i < len(t.alignments) {
			align = t.alignments[i]
		}

		if align < 0 {
			sb.WriteString(fmt.Sprintf("%-*s", width, cell))
		} else {
			sb.WriteString(fmt.Sprintf("%*s", width, cell))
		}
	}
	return sb.String()
}
