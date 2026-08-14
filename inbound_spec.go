package main

import (
	"fmt"
	"strings"
)

// normalizedSpec is a NewInboundSpec after validation and default values have
// been applied. Both backends start here: native mode turns it into a
// nativeInbound, while 3x-ui converts it into the panel's add payload.
type normalizedSpec struct {
	Protocol string
	Network  string
	Security string
	Port     int
	Path     string
	Host     string
	Remark   string
	Flow     string
}

// normalizeInboundSpec validates protocol combinations and fills defaults.
//
// used contains ports that are already occupied. If no port is supplied, a
// random unused port is selected. This logic is shared by both backends so the
// 3x-ui path cannot drift from native-mode validation.
func normalizeInboundSpec(spec NewInboundSpec, used map[int]bool) (*normalizedSpec, error) {
	proto := strings.ToLower(strings.TrimSpace(spec.Protocol))
	if proto == "" {
		proto = "vless"
	}
	if !nativeProtocols[proto] {
		return nil, fmt.Errorf("unsupported protocol %q", spec.Protocol)
	}
	network := strings.ToLower(strings.TrimSpace(spec.Network))
	if network == "" {
		network = "tcp"
	}
	if !nativeNetworks[network] {
		return nil, fmt.Errorf("unsupported transport %q", spec.Network)
	}
	security := strings.ToLower(strings.TrimSpace(spec.Security))
	if security == "" {
		security = "none"
	}
	if !nativeSecurities[security] {
		return nil, fmt.Errorf("unsupported security layer %q", spec.Security)
	}
	// REALITY works by imitating a TLS handshake. It is not meaningful on
	// transports with their own framing, and Xray rejects those combinations.
	if security == "reality" && network != "tcp" && network != "xhttp" && network != "grpc" {
		return nil, fmt.Errorf("REALITY does not support %s transport", network)
	}
	// VMess includes its own encryption, but TLS here is primarily for traffic
	// camouflage rather than cipher strength; vmess+ws+tls is a valid common combination.

	port := spec.Port
	if port == 0 {
		p, err := freeRandomPort(used)
		if err != nil {
			return nil, err
		}
		port = p
	} else if used[port] {
		return nil, fmt.Errorf("port %d is already used by another inbound", port)
	}

	path := strings.TrimSpace(spec.Path)
	if path == "" {
		switch network {
		case "ws", "httpupgrade", "xhttp":
			path = "/" + randomHex(6)
		case "grpc":
			path = randomHex(6)
		}
	}

	remark := strings.TrimSpace(spec.Remark)
	if remark == "" {
		remark = fmt.Sprintf("%s-%d", proto, port)
	}

	flow := ""
	if spec.Vision {
		if !visionCapable(proto, network, security) {
			return nil, fmt.Errorf("xtls-rprx-vision can only be used with VLESS + TCP + TLS/REALITY")
		}
		flow = "xtls-rprx-vision"
	}

	return &normalizedSpec{
		Protocol: proto,
		Network:  network,
		Security: security,
		Port:     port,
		Path:     path,
		Host:     strings.TrimSpace(spec.Host),
		Remark:   remark,
		Flow:     flow,
	}, nil
}
