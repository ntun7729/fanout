package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"v1.0.5", "v1.0.6", true},
		{"v1.0.6", "v1.0.6", false},
		{"v1.0.6", "v1.0.5", false},
		{"1.0.0", "v1.2.0", true},
		{"v2.0.0", "v1.9.9", false},
		{"dev", "v1.0.0", true},
		{"", "v1.0.0", true},
		{"v1.0.0", "", false},
		{"v1.0", "v1.0.1", true},
	}
	for _, c := range cases {
		if got := versionLess(c.cur, c.latest); got != c.want {
			t.Errorf("versionLess(%q,%q)=%v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	if v, ok := parseSemver("v1.2.3"); !ok || v != [3]int{1, 2, 3} {
		t.Fatalf("parseSemver v1.2.3 => %v %v", v, ok)
	}
	if v, ok := parseSemver("1.4.0-rc1"); !ok || v != [3]int{1, 4, 0} {
		t.Fatalf("parseSemver should ignore prerelease suffix => %v %v", v, ok)
	}
	if _, ok := parseSemver("nightly"); ok {
		t.Fatal("invalid version should fail to parse")
	}
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	tgz := filepath.Join(dir, "pkg.tar.gz")
	writeTarGz(t, tgz, map[string]string{
		"install.sh": "echo hi",
		"fanout":     "BINARY-CONTENT",
		"README.md":  "readme",
	})
	out := filepath.Join(dir, "fanout-extracted")
	if err := extractBinary(tgz, "fanout", out); err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	blob, _ := os.ReadFile(out)
	if string(blob) != "BINARY-CONTENT" {
		t.Fatalf("extracted content is incorrect: %q", blob)
	}
	if err := extractBinary(tgz, "nonexistent", out); err == nil {
		t.Fatal("missing archive member should return an error")
	}
}

func TestSha256FromList(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "checksums.txt")
	os.WriteFile(list, []byte(
		"abc123  fanout-linux-amd64.tar.gz\n"+
			"def456  fanout-linux-arm64.tar.gz\n"), 0644)
	got, err := sha256FromList(list, "fanout-linux-arm64.tar.gz")
	if err != nil || got != "def456" {
		t.Fatalf("sha256FromList => %q %v", got, err)
	}
	if _, err := sha256FromList(list, "missing.tar.gz"); err == nil {
		t.Fatal("missing checksum entry should return an error")
	}
}

func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body))})
		tw.Write([]byte(body))
	}
}
