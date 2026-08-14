package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// xrayCandidates lists Xray binary locations in priority order. Prefer the copy
// installed by fanout so another installation cannot unexpectedly change versions.
func xrayCandidates(workDir string) []string {
	return []string{
		filepath.Join(workDir, "bin", "xray"),
		"/usr/local/bin/xray",
		"/usr/bin/xray",
		// 3x-ui usually bundles an architecture-suffixed Xray binary here.
		fmt.Sprintf("/usr/local/x-ui/bin/xray-%s-%s", runtime.GOOS, xuiArchSuffix()),
	}
}

// xuiArchSuffix maps Go's GOARCH to the suffix used by 3x-ui Xray binaries.
func xuiArchSuffix() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm32"
	case "s390x":
		return "s390x"
	default:
		return runtime.GOARCH
	}
}

// findXray locates an executable Xray binary.
func findXray(workDir string) (string, error) {
	for _, p := range xrayCandidates(workDir) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p, nil
		}
	}
	if p, err := exec.LookPath("xray"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("Xray executable not found; install it at %s or /usr/local/bin/xray",
		filepath.Join(workDir, "bin", "xray"))
}

// buildXrayConfig generates a complete Xray configuration from inbounds and
// current tunnels. It creates one SOCKS outbound per connected tunnel, plus
// direct and block outbounds, and writes bindings as routing rules.
func buildXrayConfig(inbounds []*nativeInbound, tunnels []*Tunnel) map[string]any {
	live := map[string]bool{}
	for _, t := range tunnels {
		if t.Status == "up" {
			live[sanitizeTag(t.Node.HostName)] = true
		}
	}

	ins := make([]any, 0, len(inbounds))
	for _, ib := range inbounds {
		if !ib.Enable {
			continue
		}
		ins = append(ins, nativeInboundJSON(ib))
	}

	// Force IPv4 for direct traffic. Tunnels do not provide IPv6 routing, so an
	// unmatched IPv6 connection could otherwise leave through the host and expose it.
	outs := []any{
		map[string]any{
			"tag":      "direct",
			"protocol": "freedom",
			"settings": map[string]any{"domainStrategy": "UseIPv4"},
		},
		map[string]any{"tag": "block", "protocol": "blackhole"},
	}
	for _, t := range tunnels {
		if t.Status != "up" {
			continue
		}
		outs = append(outs, map[string]any{
			"tag":      tunnelTag(t),
			"protocol": "socks",
			"settings": map[string]any{
				"servers": []any{socksServerJSON(t)},
			},
		})
	}

	rules := []any{}
	for _, ib := range inbounds {
		if !ib.Enable || ib.BoundTo == "" || !live[ib.BoundTo] {
			continue
		}
		rules = append(rules, map[string]any{
			"type":        "field",
			"inboundTag":  []any{ib.tag()},
			"outboundTag": xuiTagPrefix + ib.BoundTo,
		})
	}

	return map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"inbounds":  ins,
		"outbounds": outs,
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
	}
}

// nativeInboundJSON converts one stored inbound into Xray inbound configuration.
func nativeInboundJSON(ib *nativeInbound) map[string]any {
	settings := map[string]any{}
	clients := make([]any, 0, len(ib.Clients))
	for _, c := range ib.Clients {
		if !c.Enable {
			continue
		}
		switch ib.Protocol {
		case "trojan":
			clients = append(clients, map[string]any{"password": c.Password, "email": c.Email})
		case "vmess":
			clients = append(clients, map[string]any{"id": c.ID, "email": c.Email})
		default: // vless
			clients = append(clients, map[string]any{"id": c.ID, "email": c.Email, "flow": c.Flow})
		}
	}
	settings["clients"] = clients
	if ib.Protocol == "vless" {
		settings["decryption"] = "none"
	}

	return map[string]any{
		"tag":            ib.tag(),
		"listen":         "0.0.0.0",
		"port":           ib.Port,
		"protocol":       ib.Protocol,
		"settings":       settings,
		"streamSettings": streamSettingsJSON(ib),
		"sniffing":       map[string]any{"enabled": true, "destOverride": []any{"http", "tls"}},
	}
}

// streamSettingsJSON builds transport and security settings.
func streamSettingsJSON(ib *nativeInbound) map[string]any {
	network := ib.netOrTCP()
	stream := map[string]any{"network": network, "security": ib.securityOrNone()}

	path := ib.Path
	if path == "" {
		path = "/"
	}
	switch network {
	case "ws":
		ws := map[string]any{"path": path}
		if ib.Host != "" {
			ws["host"] = ib.Host
		}
		stream["wsSettings"] = ws
	case "httpupgrade":
		hu := map[string]any{"path": path}
		if ib.Host != "" {
			hu["host"] = ib.Host
		}
		stream["httpupgradeSettings"] = hu
	case "xhttp":
		xh := map[string]any{"path": path, "mode": "auto"}
		if ib.Host != "" {
			xh["host"] = ib.Host
		}
		stream["xhttpSettings"] = xh
	case "grpc":
		// gRPC uses serviceName rather than path; reuse Path to keep the model simple.
		name := strings.TrimPrefix(ib.Path, "/")
		stream["grpcSettings"] = map[string]any{"serviceName": name}
	}

	switch ib.securityOrNone() {
	case "tls":
		if ib.TLS != nil {
			t := map[string]any{
				"certificates": []any{map[string]any{
					"certificateFile": ib.TLS.CertFile,
					"keyFile":         ib.TLS.KeyFile,
				}},
			}
			if ib.TLS.ServerName != "" {
				t["serverName"] = ib.TLS.ServerName
			}
			stream["tlsSettings"] = t
		}
	case "reality":
		if ib.Reality != nil {
			r := map[string]any{
				"dest":        ib.Reality.Dest,
				"serverNames": toAnySlice(ib.Reality.ServerNames),
				"privateKey":  ib.Reality.PrivateKey,
				"shortIds":    toAnySlice(ib.Reality.ShortIDs),
			}
			stream["realitySettings"] = r
		}
	}
	return stream
}

func toAnySlice(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

// writeXrayConfig writes the generated configuration and returns its path.
func writeXrayConfig(dir string, cfg map[string]any) (string, error) {
	blob, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "xray.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// verifyXrayConfig validates configuration with Xray before restarting. This
// prevents one bad config from killing the current process and taking all links down.
func verifyXrayConfig(bin, cfgPath string) error {
	out, err := exec.Command(bin, "run", "-test", "-c", cfgPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Xray configuration validation failed: %s", trimOutput(out))
	}
	return nil
}

func trimOutput(b []byte) string {
	s := string(b)
	if len(s) > 400 {
		s = s[:400] + "..."
	}
	return s
}

// xrayProc manages the Xray process in native mode.
type xrayProc struct {
	bin  string
	dir  string
	cmd  *exec.Cmd
	logf *os.File
}

// restart restarts Xray using a configuration that has already been written and validated.
func (p *xrayProc) restart(cfgPath string) error {
	p.stop()

	logPath := filepath.Join(p.dir, "xray.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to open Xray log: %w", err)
	}

	cmd := exec.Command(p.bin, "run", "-c", cfgPath)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		f.Close()
		return fmt.Errorf("failed to start Xray: %w", err)
	}
	p.cmd, p.logf = cmd, f
	go cmd.Wait() // Reap the child process to avoid zombies.
	p.writePID(cmd.Process.Pid)

	// Detect processes that start successfully but immediately exit.
	time.Sleep(400 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return fmt.Errorf("Xray exited immediately after startup; see %s", logPath)
	}
	return nil
}

func (p *xrayProc) stop() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		p.cmd = nil
	}
	if p.logf != nil {
		p.logf.Close()
		p.logf = nil
	}
	_ = os.Remove(p.pidPath())
}

func (p *xrayProc) pidPath() string { return filepath.Join(p.dir, "xray.pid") }

func (p *xrayProc) writePID(pid int) {
	_ = os.WriteFile(p.pidPath(), []byte(strconv.Itoa(pid)), 0600)
}

// reapOrphan removes an Xray process left behind by a prior forced termination.
// It verifies the executable path from the pidfile before killing anything so a
// recycled PID cannot terminate an unrelated process.
func (p *xrayProc) reapOrphan() {
	blob, err := os.ReadFile(p.pidPath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(blob)))
	if err != nil || pid <= 1 {
		_ = os.Remove(p.pidPath())
		return
	}
	if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		if exe == p.bin {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Kill()
			}
		}
	}
	_ = os.Remove(p.pidPath())
}
