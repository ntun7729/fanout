package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const (
	masqueNodeMarker = "masque:warp-http2"
	warpMasqueHost   = "warp-masque-tcp"
)

// warpMasqueNode exposes Cloudflare WARP as an optional exit source. Unlike
// VPN Gate, WARP chooses the egress location automatically; the WARP region is
// therefore a transport choice rather than a selectable country.
func warpMasqueNode() Node {
	return Node{
		HostName:    warpMasqueHost,
		IP:          "162.159.198.2",
		Country:     "Cloudflare WARP (MASQUE TCP)",
		CountryCode: "WARP",
		Config:      masqueNodeMarker,
	}
}

func isMasqueNode(n Node) bool {
	return n.Config == masqueNodeMarker
}

// findUsqueBinary searches fanout's managed location first, then common paths
// used by manual installations. FANOUT_USQUE_BIN overrides all discovery.
func findUsqueBinary(workDir string) (string, error) {
	if p := os.Getenv("FANOUT_USQUE_BIN"); p != "" {
		if executableFile(p) {
			return p, nil
		}
		return "", fmt.Errorf("FANOUT_USQUE_BIN points to a missing or non-executable file: %s", p)
	}

	candidates := []string{
		filepath.Join(workDir, "bin", "usque"),
		"/opt/usque/usque",
		"/usr/local/bin/usque",
	}
	for _, p := range candidates {
		if executableFile(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("usque"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("usque is not installed; rerun fanout's installer or set FANOUT_USQUE_BIN")
}

func executableFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode().Perm()&0111 != 0
}

// findUsqueConfig supports both fanout-managed registrations and the path used
// by standalone usque tests. FANOUT_USQUE_CONFIG overrides discovery.
func findUsqueConfig(workDir string) (string, error) {
	if p := os.Getenv("FANOUT_USQUE_CONFIG"); p != "" {
		if regularFile(p) {
			return p, nil
		}
		return "", fmt.Errorf("FANOUT_USQUE_CONFIG points to a missing file: %s", p)
	}

	candidates := []string{
		filepath.Join(workDir, "usque", "config.json"),
		"/var/lib/usque/config.json",
		"/etc/usque/config.json",
	}
	for _, p := range candidates {
		if regularFile(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("WARP MASQUE is not registered; create %s or set FANOUT_USQUE_CONFIG", filepath.Join(workDir, "usque", "config.json"))
}

func regularFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func freeLoopbackPort(avoid int) (int, error) {
	for i := 0; i < 8; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		if port != avoid {
			return port, nil
		}
	}
	return 0, fmt.Errorf("failed to allocate an internal WARP proxy port")
}

// startMasque launches usque in SOCKS5 mode with MASQUE carried over
// HTTP/2 + TCP/TLS. The listener is loopback-only and exists solely as an
// internal upstream for fanout's authenticated SOCKS5 listener.
func (t *Tunnel) startMasque(workDir string) error {
	if !isMasqueNode(t.Node) {
		return nil
	}

	bin, err := findUsqueBinary(workDir)
	if err != nil {
		return err
	}
	cfg, err := findUsqueConfig(workDir)
	if err != nil {
		return fmt.Errorf("%w; register once with: mkdir -p %s && cd %s && %s register --accept-tos", err, filepath.Join(workDir, "usque"), filepath.Join(workDir, "usque"), bin)
	}
	port, err := freeLoopbackPort(t.Port)
	if err != nil {
		return err
	}

	logPath := filepath.Join(workDir, fmt.Sprintf("fo%d-usque.log", t.Slot))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create usque log: %w", err)
	}

	cmd := exec.Command(bin,
		"-c", cfg,
		"socks",
		"--http2",
		"-b", "127.0.0.1",
		"-p", strconv.Itoa(port),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("failed to start usque: %w", err)
	}
	t.masque = cmd
	t.masquePort = port
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.stopMasque()
			return fmt.Errorf("usque exited before its SOCKS listener became ready; see %s", logPath)
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.stopMasque()
	return fmt.Errorf("timed out waiting for usque SOCKS listener; see %s", logPath)
}

func (t *Tunnel) masqueProxyURL() (string, error) {
	if t.masquePort <= 0 {
		return "", fmt.Errorf("WARP MASQUE proxy is not running")
	}
	return fmt.Sprintf("socks5://127.0.0.1:%d", t.masquePort), nil
}

func (t *Tunnel) stopMasque() {
	if t.masque != nil && t.masque.Process != nil {
		_ = t.masque.Process.Kill()
	}
	t.masque = nil
	t.masquePort = 0
}
