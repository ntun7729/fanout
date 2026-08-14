package main

import (
	"strings"
	"testing"
)

func TestNativeInboundTagMatchesXUIFormat(t *testing.T) {
	// Tag format must match 3x-ui so binding semantics stay consistent across backends.
	cases := []struct {
		ib   nativeInbound
		want string
	}{
		{nativeInbound{Port: 443, Network: "tcp"}, "in-443-tcp"},
		{nativeInbound{Port: 8080, Network: "ws"}, "in-8080-ws"},
		{nativeInbound{Port: 1234}, "in-1234-tcp"}, // Default to TCP.
	}
	for _, c := range cases {
		if got := c.ib.tag(); got != c.want {
			t.Errorf("tag() = %q, want %q", got, c.want)
		}
	}
}

func TestBuildXrayConfigBindsOnlyLiveTunnels(t *testing.T) {
	up := &Tunnel{Port: 1080, Status: "up", Node: Node{HostName: "jp1"}}
	down := &Tunnel{Port: 1081, Status: "failed", Node: Node{HostName: "jp2"}}
	inbounds := []*nativeInbound{
		{ID: 1, Port: 100, Protocol: "vless", Enable: true, BoundTo: "jp1"},
		{ID: 2, Port: 200, Protocol: "vless", Enable: true, BoundTo: "jp2"},
		{ID: 3, Port: 300, Protocol: "vless", Enable: true},
	}

	cfg := buildXrayConfig(inbounds, []*Tunnel{up, down})

	outs := map[string]bool{}
	for _, o := range cfg["outbounds"].([]any) {
		outs[o.(map[string]any)["tag"].(string)] = true
	}
	if !outs["fanout-jp1"] {
		t.Error("a connected tunnel should have a corresponding outbound")
	}
	if outs["fanout-jp2"] {
		t.Error("a disconnected tunnel should not generate an outbound")
	}

	rules := cfg["routing"].(map[string]any)["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("only inbounds bound to connected tunnels should have rules; got %d", len(rules))
	}
	if got := rules[0].(map[string]any)["outboundTag"]; got != "fanout-jp1" {
		t.Errorf("outboundTag = %v, want fanout-jp1", got)
	}
}

func TestBuildXrayConfigForcesIPv4OnDirect(t *testing.T) {
	// Tunnels have no IPv6 route; direct IPv6 would expose the host's real address.
	cfg := buildXrayConfig(nil, nil)
	for _, o := range cfg["outbounds"].([]any) {
		m := o.(map[string]any)
		if m["tag"] != "direct" {
			continue
		}
		s := m["settings"].(map[string]any)
		if s["domainStrategy"] != "UseIPv4" {
			t.Errorf("direct outbound should force IPv4, got %v", s["domainStrategy"])
		}
		return
	}
	t.Fatal("direct outbound was not found")
}

func TestShareLinkPerProtocol(t *testing.T) {
	c := nativeClient{ID: "uuid-1", Password: "pw-1", Email: "e", Enable: true}

	vless := shareLink(&nativeInbound{Port: 100, Protocol: "vless", Remark: "r"}, c, "1.2.3.4")
	if !strings.HasPrefix(vless, "vless://uuid-1@1.2.3.4:100?") {
		t.Errorf("invalid VLESS link format: %s", vless)
	}
	if !strings.Contains(vless, "encryption=none") {
		t.Errorf("VLESS link should contain encryption=none: %s", vless)
	}

	tro := shareLink(&nativeInbound{Port: 200, Protocol: "trojan", Network: "ws", Path: "/p"}, c, "h")
	if !strings.HasPrefix(tro, "trojan://pw-1@h:200?") {
		t.Errorf("Trojan should use the password rather than UUID: %s", tro)
	}
	if !strings.Contains(tro, "path=%2Fp") {
		t.Errorf("WebSocket link should include path: %s", tro)
	}
}

func TestCloneRemark(t *testing.T) {
	if got := cloneRemark("LineA", "JP-244"); got != "LineA-JP-244" {
		t.Errorf("cloneRemark = %q", got)
	}
	if got := cloneRemark("", "JP-244"); got != "JP-244" {
		t.Errorf("empty base remark should use the label directly, got %q", got)
	}
}

func TestVisionCapable(t *testing.T) {
	// Vision works only with VLESS + raw TCP + TLS/REALITY.
	if !visionCapable("vless", "tcp", "reality") {
		t.Error("vless/tcp/reality should support Vision")
	}
	if !visionCapable("vless", "tcp", "tls") {
		t.Error("vless/tcp/tls should support Vision")
	}
	if visionCapable("vless", "ws", "tls") {
		t.Error("WebSocket should not support Vision")
	}
	if visionCapable("vless", "tcp", "none") {
		t.Error("Vision should not be supported without a security layer")
	}
	if visionCapable("trojan", "tcp", "tls") {
		t.Error("Vision is specific to VLESS")
	}
}

func TestStreamSettingsPerNetwork(t *testing.T) {
	cases := []struct {
		ib      nativeInbound
		key     string
		wantKey string
		want    any
	}{
		{nativeInbound{Network: "ws", Path: "/p"}, "wsSettings", "path", "/p"},
		{nativeInbound{Network: "httpupgrade", Path: "/h"}, "httpupgradeSettings", "path", "/h"},
		{nativeInbound{Network: "xhttp", Path: "/x"}, "xhttpSettings", "path", "/x"},
		// gRPC has no path; Path is reused as serviceName without a leading slash.
		{nativeInbound{Network: "grpc", Path: "/svc"}, "grpcSettings", "serviceName", "svc"},
	}
	for _, c := range cases {
		st := streamSettingsJSON(&c.ib)
		sub, ok := st[c.key].(map[string]any)
		if !ok {
			t.Errorf("%s is missing %s", c.ib.Network, c.key)
			continue
		}
		if got := sub[c.wantKey]; got != c.want {
			t.Errorf("%s %s = %v, want %v", c.ib.Network, c.wantKey, got, c.want)
		}
	}
}

func TestStreamSettingsReality(t *testing.T) {
	ib := nativeInbound{
		Network: "tcp", Security: "reality",
		Reality: &realityConfig{
			Dest: "www.cloudflare.com:443", ServerNames: []string{"www.cloudflare.com"},
			PrivateKey: "priv", PublicKey: "pub", ShortIDs: []string{"abcd1234"},
		},
	}
	st := streamSettingsJSON(&ib)
	if st["security"] != "reality" {
		t.Fatalf("security = %v", st["security"])
	}
	r, ok := st["realitySettings"].(map[string]any)
	if !ok {
		t.Fatal("realitySettings is missing")
	}
	if r["privateKey"] != "priv" {
		t.Errorf("server config should contain the private key, got %v", r["privateKey"])
	}
	// Public keys are client-only; including one in server config makes Xray reject it.
	if _, leaked := r["publicKey"]; leaked {
		t.Error("server configuration should not contain publicKey")
	}
}

func TestShareLinkCarriesSecurityParams(t *testing.T) {
	c := nativeClient{ID: "uuid-1", Enable: true, Flow: "xtls-rprx-vision"}

	re := shareLink(&nativeInbound{
		Port: 100, Protocol: "vless", Network: "tcp", Security: "reality", Remark: "r",
		Reality: &realityConfig{
			ServerNames: []string{"www.cloudflare.com"}, PublicKey: "PBK",
			ShortIDs: []string{"sid1"}, Fingerprint: "chrome",
		},
	}, c, "h")
	for _, want := range []string{"pbk=PBK", "sid=sid1", "fp=chrome",
		"sni=www.cloudflare.com", "flow=xtls-rprx-vision"} {
		if !strings.Contains(re, want) {
			t.Errorf("REALITY link is missing %s: %s", want, re)
		}
	}

	// Self-signed TLS links require certificate pinning or clients cannot validate the certificate.
	tl := shareLink(&nativeInbound{
		Port: 200, Protocol: "vless", Network: "tcp", Security: "tls", Remark: "t",
		TLS: &tlsConfig{ServerName: "demo.local", SelfSigned: true, CertSha256: "AABB"},
	}, nativeClient{ID: "u", Enable: true}, "h")
	if !strings.Contains(tl, "pinSHA256=AABB") {
		t.Errorf("self-signed TLS link should include the certificate fingerprint: %s", tl)
	}
}
