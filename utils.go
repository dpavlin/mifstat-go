package main

import (
	"fmt"
)

func formatRate(kbps float64) string {
	if kbps >= 1024*1024 {
		return fmt.Sprintf("%.2f GB/s", kbps/(1024*1024))
	}
	if kbps >= 1024 {
		return fmt.Sprintf("%.2f MB/s", kbps/1024)
	}
	return fmt.Sprintf("%.2f KB/s", kbps)
}

func calcEMA(current, prev, alpha float64) float64 {
	if prev == 0 {
		return current
	}
	return (current * alpha) + (prev * (1.0 - alpha))
}
