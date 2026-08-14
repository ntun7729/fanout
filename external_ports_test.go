package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeXrayConfigPorts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := `{
	  "inbounds": [
	    {"port": 15331, "protocol": "vless"},
	    {"port": 8080, "protocol": "trojan"},
	    {"port": "1000-2000", "protocol": "dokodemo-door"},
	    {"protocol": "no-port"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	used := map[int]bool{}
	mergeXrayConfigPorts(path, used)
	if !used[15331] || !used[8080] {
		t.Fatalf("numeric ports should be included: %v", used)
	}
	if used[1000] || used[2000] {
		t.Errorf("port-range strings should not be parsed as individual ports: %v", used)
	}
	if len(used) != 2 {
		t.Errorf("expected exactly 2 ports, got %v", used)
	}
}

func TestMergeXrayConfigPortsMissingFileIsSilent(t *testing.T) {
	used := map[int]bool{}
	mergeXrayConfigPorts("/nonexistent/definitely/not/here.json", used)
	if len(used) != 0 {
		t.Errorf("missing files should be ignored, got %v", used)
	}
}

func TestMergeXrayConfigPortsBadJSONIsSilent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json at all"), 0600); err != nil {
		t.Fatal(err)
	}
	used := map[int]bool{}
	mergeXrayConfigPorts(path, used)
	if len(used) != 0 {
		t.Errorf("invalid JSON should be ignored, got %v", used)
	}
}
