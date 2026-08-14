package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// XCL integrates with the system-level Xray installation created by
// byJoey/xray-cf-lite.
//
// xray-cf-lite installs Xray as a system service with configuration at
// /usr/local/etc/xray/config.json. It creates several WebSocket inbounds behind
// Cloudflare, while its outbounds are normally only direct/block. In this mode,
// fanout does not create or delete nodes. It only adds routing that selects which
// residential exit each inbound uses. fanout touches only its own fanout-prefixed
// SOCKS outbounds and routing rules, leaving xray-cf-lite-managed inbounds intact.
type XCL struct {
	cfgPath string
	initSys string // "systemd" or "openrc"
	svcName string // xray-cf-lite service name; normally "xray"
	// restart is a function field so tests can avoid restarting a real system service.
	restart func() error
}

const (
	xclConfigPath = "/usr/local/etc/xray/config.json"
	xclStateDir   = "/etc/xray-cf-lite"
)

// DetectXCL reports xray-cf-lite only when both its state directory and system
// Xray configuration exist, avoiding false detection of manually installed Xray.
func DetectXCL() (*XCL, error) {
	if _, err := os.Stat(xclStateDir); err != nil {
		return nil, fmt.Errorf("xray-cf-lite state directory was not found: %s", xclStateDir)
	}
	if _, err := os.Stat(xclConfigPath); err != nil {
		return nil, fmt.Errorf("Xray configuration was not found: %s", xclConfigPath)
	}
	x := &XCL{cfgPath: xclConfigPath, svcName: "xray"}
	x.initSys = detectInitSystem()
	x.restart = x.restartXray
	// Read once during detection so broken configuration is reported early.
	if _, err := x.loadCfg(); err != nil {
		return nil, err
	}
	return x, nil
}

// detectInitSystem identifies systemd or OpenRC, matching xray-cf-lite behavior.
func detectInitSystem() string {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return "systemd"
	}
	return "openrc"
}

func (x *XCL) Kind() string { return "xray-cf-lite" }

func (x *XCL) Describe() string {
	return fmt.Sprintf("xray-cf-lite managed (%s, config %s)", x.initSys, x.cfgPath)
}

// loadCfg reads and parses xray-cf-lite's config.json.
func (x *XCL) loadCfg() (map[string]any, error) {
	blob, err := os.ReadFile(x.cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", x.cfgPath, err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", x.cfgPath, err)
	}
	return cfg, nil
}

// saveCfg atomically writes configuration and restarts the system Xray service.
func (x *XCL) saveCfg(cfg map[string]any) error {
	blob, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize configuration: %w", err)
	}
	tmp := x.cfgPath + ".fanout.tmp"
	if err := os.WriteFile(tmp, blob, 0644); err != nil {
		return fmt.Errorf("failed to write temporary configuration: %w", err)
	}
	if err := os.Rename(tmp, x.cfgPath); err != nil {
		return fmt.Errorf("failed to replace configuration: %w", err)
	}
	if x.restart == nil {
		return x.restartXray()
	}
	return x.restart()
}

func (x *XCL) restartXray() error {
	var cmd *exec.Cmd
	if x.initSys == "systemd" {
		cmd = exec.Command("systemctl", "restart", x.svcName)
	} else {
		cmd = exec.Command("rc-service", x.svcName, "restart")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restart %s: %s", x.svcName, trimOutput(out))
	}
	return nil
}

// xclInboundTag returns an inbound's tag, falling back to protocol and port.
func xclInboundTag(m map[string]any) string {
	if tag, _ := m["tag"].(string); tag != "" {
		return tag
	}
	proto, _ := m["protocol"].(string)
	port := jsonInt(m["port"])
	return fmt.Sprintf("in-%s-%d", proto, port)
}

func jsonInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return int(n)
		}
	case int:
		return t
	}
	return 0
}

// Inbounds lists xray-cf-lite inbounds and their current exit bindings.
func (x *XCL) Inbounds(live map[string]bool) ([]Inbound, error) {
	cfg, err := x.loadCfg()
	if err != nil {
		return nil, err
	}
	bound := x.boundInbounds(cfg)

	rawIns, _ := cfg["inbounds"].([]any)
	out := make([]Inbound, 0, len(rawIns))
	for _, raw := range rawIns {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tag := xclInboundTag(m)
		proto, _ := m["protocol"].(string)
		port := jsonInt(m["port"])
		out = append(out, Inbound{
			ID:       port, // xray-cf-lite has no numeric ID; use port as a stable identifier.
			Port:     port,
			Protocol: proto,
			Remark:   tag,
			Enable:   true,
			Tag:      tag,
			BoundTo:  bound[tag],
			BoundUp:  live[bound[tag]],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// boundInbounds reconstructs inboundTag -> node hostname mappings from routing rules.
func (x *XCL) boundInbounds(cfg map[string]any) map[string]string {
	bound := map[string]string{}
	routing, _ := cfg["routing"].(map[string]any)
	if routing == nil {
		return bound
	}
	rules, _ := routing["rules"].([]any)
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		tag, _ := m["outboundTag"].(string)
		if !strings.HasPrefix(tag, xuiTagPrefix) {
			continue
		}
		host := strings.TrimPrefix(tag, xuiTagPrefix)
		if isAllDigits(host) {
			continue
		}
		for _, it := range toStringSlice(m["inboundTag"]) {
			bound[it] = host
		}
	}
	return bound
}

// syncOutbounds aligns fanout-prefixed outbounds with connected tunnels while
// preserving xray-cf-lite's own direct/block configuration.
func (x *XCL) syncOutbounds(cfg map[string]any, tunnels []*Tunnel) {
	outbounds, _ := cfg["outbounds"].([]any)
	kept := make([]any, 0, len(outbounds))
	for _, ob := range outbounds {
		m, ok := ob.(map[string]any)
		if !ok {
			kept = append(kept, ob)
			continue
		}
		tag, _ := m["tag"].(string)
		if !strings.HasPrefix(tag, xuiTagPrefix) {
			forceIPv4(m)
			kept = append(kept, ob)
		}
	}
	for _, t := range tunnels {
		if t.Status != "up" {
			continue
		}
		kept = append(kept, map[string]any{
			"tag":      tunnelTag(t),
			"protocol": "socks",
			"settings": map[string]any{
				"servers": []any{socksServerJSON(t)},
			},
		})
	}
	cfg["outbounds"] = kept
}

// Bind routes an inbound through a specified tunnel. Empty hostname unbinds it
// and restores xray-cf-lite's direct routing.
func (x *XCL) Bind(inboundTag string, hostname string, tunnels []*Tunnel) error {
	var target *Tunnel
	if hostname != "" {
		for _, t := range tunnels {
			if t.Node.HostName == hostname {
				target = t
				break
			}
		}
		if target == nil {
			return fmt.Errorf("node %s has no running tunnel", hostname)
		}
		if target.Status != "up" {
			return fmt.Errorf("tunnel for node %s is not connected yet (current status: %s)", hostname, target.Status)
		}
	}

	live := map[string]bool{}
	for _, t := range tunnels {
		if t.Status == "up" {
			live[sanitizeTag(t.Node.HostName)] = true
		}
	}
	current, err := x.Inbounds(live)
	if err != nil {
		return err
	}
	knownTags := map[string]bool{}
	for _, ib := range current {
		knownTags[ib.Tag] = true
	}
	if !knownTags[inboundTag] {
		return fmt.Errorf("inbound %s does not exist", inboundTag)
	}

	cfg, err := x.loadCfg()
	if err != nil {
		return err
	}
	x.syncOutbounds(cfg, tunnels)

	routing, _ := cfg["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
	}
	rules, _ := routing["rules"].([]any)

	cleaned := make([]any, 0, len(rules)+1)
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			cleaned = append(cleaned, r)
			continue
		}
		outTag, _ := m["outboundTag"].(string)
		if !strings.HasPrefix(outTag, xuiTagPrefix) {
			cleaned = append(cleaned, r)
			continue
		}
		remain := []any{}
		for _, it := range toStringSlice(m["inboundTag"]) {
			if it != inboundTag && knownTags[it] {
				remain = append(remain, it)
			}
		}
		if len(remain) > 0 {
			m["inboundTag"] = remain
			cleaned = append(cleaned, m)
		}
	}

	if target != nil {
		cleaned = append(cleaned, map[string]any{
			"type":        "field",
			"inboundTag":  []any{inboundTag},
			"outboundTag": tunnelTag(target),
		})
	}

	routing["rules"] = cleaned
	cfg["routing"] = routing
	return x.saveCfg(cfg)
}

// Rebind moves all inbounds from oldHost to target after a tunnel switches nodes.
func (x *XCL) Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error {
	if target == nil {
		return fmt.Errorf("target node is nil")
	}
	cfg, err := x.loadCfg()
	if err != nil {
		return err
	}
	bound := x.boundInbounds(cfg)
	moved := 0
	for tag, host := range bound {
		if host == oldHost {
			if err := x.Bind(tag, target.Node.HostName, tunnels); err != nil {
				return err
			}
			moved++
		}
	}
	if moved == 0 {
		return fmt.Errorf("no inbounds are bound to %s", oldHost)
	}
	return nil
}

// ResyncOutbound refreshes fanout outbounds after tunnel state changes.
func (x *XCL) ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error {
	return x.OnTunnelsChanged(tunnels)
}

// OnTunnelsChanged rewrites fanout-prefixed outbounds and restarts Xray.
func (x *XCL) OnTunnelsChanged(tunnels []*Tunnel) error {
	cfg, err := x.loadCfg()
	if err != nil {
		return err
	}
	x.syncOutbounds(cfg, tunnels)
	return x.saveCfg(cfg)
}

// InboundDetail returns one inbound. Share links remain the responsibility of
// xray-cf-lite's own subscription system.
func (x *XCL) InboundDetail(id int, publicHost string) (*InboundDetail, error) {
	ins, err := x.Inbounds(map[string]bool{})
	if err != nil {
		return nil, err
	}
	for _, ib := range ins {
		if ib.ID == id {
			return &InboundDetail{Inbound: ib}, nil
		}
	}
	return nil, fmt.Errorf("inbound %d does not exist", id)
}

// InboundLinks does not generate links in this backend; xray-cf-lite provides them.
func (x *XCL) InboundLinks(ids []int, publicHost string) ([]string, error) {
	return nil, nil
}

// The following operations are intentionally disabled in xray-cf-lite mode.
// xray-cf-lite owns the nodes; fanout only changes routing.
var errXCLReadOnly = fmt.Errorf("nodes are managed by xray-cf-lite in this mode; fanout can only change routing (exit bindings), not create, delete, or modify nodes")

func (x *XCL) CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error) {
	return nil, errXCLReadOnly
}

func (x *XCL) CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error) {
	return nil, errXCLReadOnly
}

func (x *XCL) DeleteInbounds(ids []int, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

func (x *XCL) UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

func (x *XCL) AddClient(id int, email string, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

func (x *XCL) DeleteClient(id int, email string, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

func (x *XCL) ResetClient(id int, email string, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

// Close is a no-op because this backend does not own the Xray process.
func (x *XCL) Close() {}
