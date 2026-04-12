package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGetSwitches(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "switches-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := `
# Comment
10.0.0.1  sw1
10.0.0.2  sw2  MAC1
invalid
`
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	got := getSwitches(tmpFile.Name())
	want := []map[string]string{
		{"ip": "10.0.0.1", "name": "sw1"},
		{"ip": "10.0.0.2", "name": "sw2"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("getSwitches() = %v; want %v", got, want)
	}
}

func TestGetCommunity(t *testing.T) {
	// Test flag override
	if got := getCommunity("flag"); got != "flag" {
		t.Errorf("getCommunity(\"flag\") = %q; want \"flag\"", got)
	}

	// Test home config (mocking HomeDir)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("skipping home config test: no home dir")
	}
	configPath := filepath.Join(home, ".config", "snmp.community")
	
	// Backup existing config
	backupPath := configPath + ".bak"
	if _, err := os.Stat(configPath); err == nil {
		os.Rename(configPath, backupPath)
		defer os.Rename(backupPath, configPath)
	}

	err = os.MkdirAll(filepath.Dir(configPath), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(configPath, []byte("secret"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(configPath)

	if got := getCommunity(""); got != "secret" {
		t.Errorf("getCommunity(\"\") with config = %q; want \"secret\"", got)
	}
}

func TestGetSlowMs(t *testing.T) {
	tests := []struct {
		val      int64
		delay    float64
		isSet    bool
		expected int64
	}{
		{500, 1.0, true, 500},   // explicitly set to 500
		{800, 2.0, true, 800},   // explicitly set to 800
		{0, 1.0, false, 1000},   // not set, delay 1.0 -> 1000
		{0, 0.5, false, 500},    // not set, delay 0.5 -> 500
		{0, 2.0, false, 2000},   // not set, delay 2.0 -> 2000
		{0, 1.0, true, 0},       // explicitly set to 0 (disabled)
	}

	for _, tc := range tests {
		got := getSlowMs(tc.val, tc.delay, tc.isSet)
		if got != tc.expected {
			t.Errorf("getSlowMs(val=%d, delay=%.1f, isSet=%v) = %d; want %d", tc.val, tc.delay, tc.isSet, got, tc.expected)
		}
	}
}
