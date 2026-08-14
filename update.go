package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const updateRepo = "ntun7729/fanout"

// releaseInfo contains the GitHub Releases API fields used by the updater.
type releaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// UpdateStatus is returned to the UI with the current version, latest version,
// update availability, and release notes.
type UpdateStatus struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"has_update"`
	Notes     string `json:"notes"`
	URL       string `json:"url"`
}

// assetArch maps runtime.GOARCH to the name used in release assets.
func assetArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

// fetchLatestRelease fetches metadata for the latest release.
func fetchLatestRelease() (*releaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "fanout-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// checkUpdate compares the current version with the latest release.
func checkUpdate() (*UpdateStatus, error) {
	rel, err := fetchLatestRelease()
	if err != nil {
		return nil, err
	}
	cur := strings.TrimSpace(version)
	latest := strings.TrimSpace(rel.TagName)
	st := &UpdateStatus{
		Current:   cur,
		Latest:    latest,
		Notes:     strings.TrimSpace(rel.Body),
		URL:       rel.HTMLURL,
		HasUpdate: versionLess(cur, latest),
	}
	return st, nil
}

// versionLess reports whether cur is older than latest. vX.Y.Z versions are
// compared numerically. dev or unparseable versions conservatively report an
// available update so users can install a formal release.
func versionLess(cur, latest string) bool {
	if latest == "" {
		return false
	}
	if cur == "" || cur == "dev" {
		return true
	}
	cn, cok := parseSemver(cur)
	ln, lok := parseSemver(latest)
	if !cok || !lok {
		return cur != latest
	}
	for i := 0; i < 3; i++ {
		if cn[i] != ln[i] {
			return cn[i] < ln[i]
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Remove prerelease/build suffixes.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// applyUpdate downloads the latest package for this architecture, verifies it,
// replaces the current binary, and restarts the service. On success the init
// system starts the newly installed version.
func applyUpdate() error {
	rel, err := fetchLatestRelease()
	if err != nil {
		return err
	}

	arch := assetArch()
	assetName := fmt.Sprintf("fanout-linux-%s.tar.gz", arch)
	var assetURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case assetName:
			assetURL = a.URL
		case "checksums.txt":
			sumsURL = a.URL
		}
	}
	if assetURL == "" {
		return fmt.Errorf("latest release has no package for %s", arch)
	}

	tmp, err := os.MkdirTemp("", "fanout-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	tarPath := filepath.Join(tmp, assetName)
	if err := downloadFile(assetURL, tarPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify a checksum when available to detect corrupted or tampered packages.
	if sumsURL != "" {
		if err := verifyChecksum(tarPath, assetName, sumsURL); err != nil {
			return err
		}
	}

	newBin := filepath.Join(tmp, "fanout")
	if err := extractBinary(tarPath, "fanout", newBin); err != nil {
		return fmt.Errorf("failed to extract package: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate current executable: %w", err)
	}
	self, _ = filepath.EvalSymlinks(self)

	// Atomic replacement: stage the binary in the same directory, then rename it.
	staged := self + ".new"
	if err := copyFileMode(newBin, staged, 0755); err != nil {
		return fmt.Errorf("failed to write new version: %w", err)
	}
	if err := os.Rename(staged, self); err != nil {
		os.Remove(staged)
		return fmt.Errorf("failed to replace executable: %w", err)
	}

	// Restart asynchronously after the response has had time to reach the UI.
	go func() {
		time.Sleep(800 * time.Millisecond)
		restartSelf()
	}()
	return nil
}

func downloadFile(url, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "fanout-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func verifyChecksum(path, name, sumsURL string) error {
	sums := filepath.Join(filepath.Dir(path), "checksums.txt")
	if err := downloadFile(sumsURL, sums); err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}
	want, err := sha256FromList(sums, name)
	if err != nil {
		return err
	}
	got, err := sha256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("checksum mismatch; package may be corrupted")
	}
	return nil
}

func sha256FromList(listPath, name string) (string, error) {
	blob, err := os.ReadFile(listPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(blob), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum list does not contain %s", name)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractBinary extracts the named member from a tar.gz archive into dst.
func extractBinary(tarGz, member, dst string) error {
	f, err := os.Open(tarGz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("package does not contain %s", member)
		}
		if err != nil {
			return err
		}
		if filepath.Base(hd.Name) != member {
			continue
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		defer out.Close()
		if _, err := io.Copy(out, tr); err != nil {
			return err
		}
		return nil
	}
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// restartSelf restarts the fanout service through the active init system. It
// supports systemd and OpenRC; without either, the binary is replaced but the
// user must restart the service manually.
func restartSelf() {
	if hasCmd("systemctl") && dirExists("/run/systemd/system") {
		_ = exec.Command("systemctl", "restart", "fanout").Start()
		return
	}
	if hasCmd("rc-service") {
		_ = exec.Command("rc-service", "fanout", "restart").Start()
		return
	}
	fmt.Println("fanout: binary replaced, but systemd/OpenRC was not detected; restart the service manually")
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
