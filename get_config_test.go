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
