package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWarpMasqueNode(t *testing.T) {
	n := warpMasqueNode()
	if !isMasqueNode(n) {
		t.Fatal("warp node was not recognized as MASQUE")
	}
	if isProxyNode(n) {
		t.Fatal("warp MASQUE node must not be treated as a public proxy node")
	}
	if n.CountryCode != "WARP" {
		t.Fatalf("unexpected WARP region code %q", n.CountryCode)
	}
	if n.IP != "162.159.198.2" {
		t.Fatalf("unexpected HTTP/2 endpoint %q", n.IP)
	}
}

func TestFindUsqueConfigPrefersEnvironment(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "warp.json")
	if err := os.WriteFile(cfg, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FANOUT_USQUE_CONFIG", cfg)

	got, err := findUsqueConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("got %q, want %q", got, cfg)
	}
}

func TestFindUsqueBinaryPrefersEnvironment(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "usque")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FANOUT_USQUE_BIN", bin)

	got, err := findUsqueBinary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != bin {
		t.Fatalf("got %q, want %q", got, bin)
	}
}
