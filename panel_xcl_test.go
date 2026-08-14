package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// xclFixture creates an xray-cf-lite-style config with three WebSocket inbounds
// and only direct/block outbounds.
func xclFixture(t *testing.T) *XCL {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := `{
	  "inbounds": [
	    {"tag": "vless-ws", "port": 10001, "protocol": "vless"},
	    {"tag": "trojan-ws", "port": 10002, "protocol": "trojan"},
	    {"tag": "vmess-ws", "port": 10003, "protocol": "vmess"}
	  ],
	  "outbounds": [
	    {"tag": "direct", "protocol": "freedom"},
	    {"tag": "block", "protocol": "blackhole"}
	  ],
	  "routing": {
	    "domainStrategy": "AsIs",
	    "rules": [
	      {"type": "field", "outboundTag": "block", "protocol": ["bittorrent"]}
	    ]
	  }
	}`
	if err := os.WriteFile(path, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	return &XCL{cfgPath: path, initSys: "systemd", svcName: "xray", restart: func() error { return nil }}
}

func xclTunnel(host string, slot, port int) *Tunnel {
	return &Tunnel{Slot: slot, Port: port, Status: "up", Node: Node{HostName: host}}
}

func readCfg(t *testing.T, x *XCL) map[string]any {
	t.Helper()
	cfg, err := x.loadCfg()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func outboundTags(cfg map[string]any) []string {
	var tags []string
	obs, _ := cfg["outbounds"].([]any)
	for _, ob := range obs {
		m, _ := ob.(map[string]any)
		if m == nil {
			continue
		}
		tag, _ := m["tag"].(string)
		tags = append(tags, tag)
	}
	return tags
}

func TestXCLInboundsListsAllThree(t *testing.T) {
	x := xclFixture(t)
	ins, err := x.Inbounds(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 3 {
		t.Fatalf("expected three inbounds, got %d", len(ins))
	}
	if ins[0].Port != 10001 || ins[0].Tag != "vless-ws" {
		t.Errorf("inbounds should be sorted by port and retain tags, got %+v", ins[0])
	}
	for _, ib := range ins {
		if ib.BoundTo != "" {
			t.Errorf("initial state should have no bindings: %+v", ib)
		}
	}
}

func TestXCLBindAddsRoutingKeepsXCLOutbounds(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}

	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}

	cfg := readCfg(t, x)
	tags := outboundTags(cfg)
	if len(tags) != 3 || tags[0] != "direct" || tags[1] != "block" || tags[2] != "fanout-jp-01" {
		t.Fatalf("xray-cf-lite outbounds should remain intact with fanout appended: %v", tags)
	}

	bound := x.boundInbounds(cfg)
	if bound["vless-ws"] != "jp-01" {
		t.Fatalf("vless-ws should bind to jp-01, got %v", bound)
	}
	if len(bound) != 1 {
		t.Errorf("only one inbound should be bound, got %v", bound)
	}

	// The other inbounds have no rules and should continue using xray-cf-lite's direct route.
	ins, err := x.Inbounds(map[string]bool{"jp-01": true})
	if err != nil {
		t.Fatal(err)
	}
	for _, ib := range ins {
		if ib.Tag == "vless-ws" {
			if ib.BoundTo != "jp-01" || !ib.BoundUp {
				t.Errorf("binding state should be readable: %+v", ib)
			}
			continue
		}
		if ib.BoundTo != "" {
			t.Errorf("%s should remain unbound: %+v", ib.Tag, ib)
		}
	}
}

func TestXCLBindEachInboundToDifferentExit(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{
		xclTunnel("jp-01", 1, 20001),
		xclTunnel("us-02", 2, 20002),
	}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	if err := x.Bind("trojan-ws", "us-02", tunnels); err != nil {
		t.Fatal(err)
	}

	bound := x.boundInbounds(readCfg(t, x))
	if bound["vless-ws"] != "jp-01" || bound["trojan-ws"] != "us-02" {
		t.Fatalf("the two inbounds should bind to different exits, got %v", bound)
	}
}

func TestXCLBindRebindMovesInsteadOfDuplicating(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{
		xclTunnel("jp-01", 1, 20001),
		xclTunnel("us-02", 2, 20002),
	}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	if err := x.Bind("vless-ws", "us-02", tunnels); err != nil {
		t.Fatal(err)
	}

	cfg := readCfg(t, x)
	routing, _ := cfg["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	// Keep xray-cf-lite's Bittorrent block rule plus exactly one fanout rule.
	if len(rules) != 2 {
		blob, _ := json.Marshal(rules)
		t.Fatalf("rebinding should rewrite rather than duplicate rules: %s", blob)
	}
	if x.boundInbounds(cfg)["vless-ws"] != "us-02" {
		t.Errorf("rebound inbound should point to us-02")
	}
}

func TestXCLBindEmptyHostUnbinds(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	if err := x.Bind("vless-ws", "", tunnels); err != nil {
		t.Fatal(err)
	}
	if bound := x.boundInbounds(readCfg(t, x)); len(bound) != 0 {
		t.Fatalf("no binding rules should remain after unbinding: %v", bound)
	}
}

// fanout binding changes must not modify xray-cf-lite's own routing rules.
func TestXCLKeepsForeignRoutingRules(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	if err := x.Bind("vless-ws", "", tunnels); err != nil {
		t.Fatal(err)
	}

	cfg := readCfg(t, x)
	routing, _ := cfg["routing"].(map[string]any)
	if routing["domainStrategy"] != "AsIs" {
		t.Errorf("domainStrategy should remain unchanged: %v", routing["domainStrategy"])
	}
	rules, _ := routing["rules"].([]any)
	if len(rules) != 1 {
		blob, _ := json.Marshal(rules)
		t.Fatalf("only xray-cf-lite's Bittorrent rule should remain: %s", blob)
	}
	m, _ := rules[0].(map[string]any)
	if m["outboundTag"] != "block" {
		t.Errorf("the remaining rule should be the Bittorrent block rule: %v", m)
	}
}

func TestXCLBindRejectsUnknownInbound(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}
	if err := x.Bind("missing-tag", "jp-01", tunnels); err == nil {
		t.Fatal("binding an unknown inbound should return an error")
	}
}

func TestXCLOnTunnelsChangedDropsDeadOutbounds(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	// Once the tunnel disappears, its fanout outbound should disappear while direct/block remain.
	if err := x.OnTunnelsChanged(nil); err != nil {
		t.Fatal(err)
	}
	tags := outboundTags(readCfg(t, x))
	if len(tags) != 2 || tags[0] != "direct" || tags[1] != "block" {
		t.Fatalf("only xray-cf-lite's own outbounds should remain: %v", tags)
	}
}

func TestXCLNodeOpsAreReadOnly(t *testing.T) {
	x := xclFixture(t)
	if _, err := x.CreateInbound(NewInboundSpec{}, nil); err == nil {
		t.Error("creating nodes should be rejected")
	}
	if err := x.DeleteInbounds([]int{10001}, nil); err == nil {
		t.Error("deleting nodes should be rejected")
	}
	if err := x.UpdateInbound(10001, InboundPatch{}, nil); err == nil {
		t.Error("modifying nodes should be rejected")
	}
}

func TestConfigurePanelReadsSavedMode(t *testing.T) {
	dir := t.TempDir()
	if err := savePanelMode(dir, "xray-cf-lite"); err != nil {
		t.Fatal(err)
	}

	// Without a command-line override, restore the backend selected in the UI.
	configurePanel(dir, "")
	if got := currentPanelMode(); got != "xray-cf-lite" {
		t.Fatalf("saved UI mode should be restored after restart, got %q", got)
	}

	// An explicit command-line setting has higher priority.
	configurePanel(dir, "native")
	if got := currentPanelMode(); got != "native" {
		t.Fatalf("-panel should override the saved setting, got %q", got)
	}

	// Saving an empty value removes the selection and returns to automatic detection.
	if err := savePanelMode(dir, ""); err != nil {
		t.Fatal(err)
	}
	configurePanel(dir, "")
	if got := currentPanelMode(); got != "" {
		t.Fatalf("clearing the saved mode should restore automatic detection, got %q", got)
	}
}
