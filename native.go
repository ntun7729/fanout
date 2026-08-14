package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
)

// Native is fanout's self-managed Xray backend, used when 3x-ui is not installed.
//
// Inbound data is stored in native.json and the full Xray configuration is
// regenerated after every change. Full regeneration avoids partially applied state.
type Native struct {
	mu    sync.Mutex
	dir   string
	store *nativeStore
	proc  *xrayProc
}

func openNative(workDir string) (*Native, error) {
	if workDir == "" {
		return nil, fmt.Errorf("native mode requires a working directory")
	}
	bin, err := findXray(workDir)
	if err != nil {
		return nil, err
	}
	store, err := loadNativeStore(workDir)
	if err != nil {
		return nil, err
	}
	n := &Native{
		dir:   workDir,
		store: store,
		proc:  &xrayProc{bin: bin, dir: workDir},
	}
	// Clean up an Xray process left behind after a forced termination.
	n.proc.reapOrphan()
	return n, nil
}

func (n *Native) Kind() string { return "native" }

func (n *Native) Describe() string {
	return fmt.Sprintf("Built-in Xray (%s)", n.proc.bin)
}

// apply regenerates configuration, restarts Xray, and persists state.
// The caller must hold n.mu.
func (n *Native) apply(tunnels []*Tunnel) error {
	cfg := buildXrayConfig(n.store.sorted(), tunnels)
	path, err := writeXrayConfig(n.dir, cfg)
	if err != nil {
		return err
	}
	if err := verifyXrayConfig(n.proc.bin, path); err != nil {
		return err
	}
	// No process is needed when there are no inbounds.
	if len(cfg["inbounds"].([]any)) == 0 {
		n.proc.stop()
		return n.store.save(n.dir)
	}
	if err := n.proc.restart(path); err != nil {
		return err
	}
	return n.store.save(n.dir)
}

// OnTunnelsChanged rebuilds configuration because native outbounds are derived directly from tunnels.
func (n *Native) OnTunnelsChanged(tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.apply(tunnels)
}

// Close stops the Xray process launched by fanout.
func (n *Native) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.proc.stop()
}

func (n *Native) Inbounds(live map[string]bool) ([]Inbound, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	list := n.store.sorted()
	out := make([]Inbound, 0, len(list))
	for _, ib := range list {
		out = append(out, Inbound{
			ID: ib.ID, Port: ib.Port, Protocol: ib.Protocol,
			Remark: ib.Remark, Enable: ib.Enable, Tag: ib.tag(),
			BoundTo: ib.BoundTo, BoundUp: live[ib.BoundTo],
		})
	}
	return out, nil
}

func (n *Native) InboundDetail(id int, publicHost string) (*InboundDetail, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	ib := n.store.byID(id)
	if ib == nil {
		return nil, fmt.Errorf("inbound %d does not exist", id)
	}
	detail := &InboundDetail{
		Inbound: Inbound{
			ID: ib.ID, Port: ib.Port, Protocol: ib.Protocol,
			Remark: ib.Remark, Enable: ib.Enable, Tag: ib.tag(),
			BoundTo: ib.BoundTo,
		},
		Listen:  "0.0.0.0",
		Network: ib.netOrTCP(),
		TLS:     "none",
	}
	for _, c := range ib.Clients {
		id := c.ID
		if ib.Protocol == "trojan" {
			id = c.Password
		}
		detail.Clients = append(detail.Clients, ClientInfo{Email: c.Email, ID: id, Enable: c.Enable})
		detail.Links = append(detail.Links, shareLink(ib, c, publicHost))
	}
	return detail, nil
}

func (n *Native) InboundLinks(ids []int, publicHost string) ([]string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	var out []string
	for _, id := range ids {
		ib := n.store.byID(id)
		if ib == nil {
			continue
		}
		for _, c := range ib.Clients {
			out = append(out, shareLink(ib, c, publicHost))
		}
	}
	return out, nil
}

func (n *Native) Bind(inboundTag string, hostname string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()

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

	var found *nativeInbound
	for _, ib := range n.store.Inbounds {
		if ib.tag() == inboundTag {
			found = ib
			break
		}
	}
	if found == nil {
		return fmt.Errorf("inbound %s does not exist", inboundTag)
	}

	if target == nil {
		found.BoundTo = ""
	} else {
		found.BoundTo = sanitizeTag(target.Node.HostName)
	}
	return n.apply(tunnels)
}

func (n *Native) Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	oldTag := sanitizeTag(oldHost)
	newTag := sanitizeTag(target.Node.HostName)
	newLabel := exitLabel(target)
	for _, ib := range n.store.Inbounds {
		if ib.BoundTo != oldTag {
			continue
		}
		ib.BoundTo = newTag
		// Update the remark suffix so it reflects the new exit.
		ib.Remark = renameExitSuffix(ib.Remark, newLabel)
	}
	return n.apply(tunnels)
}

func (n *Native) ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.apply(tunnels)
}

// CloneToTunnels clones an inbound for specified tunnels and binds each copy.
// Client credentials are preserved so users only need to change the port to switch exits.
func (n *Native) CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	tpl := n.store.byID(templateID)
	if tpl == nil {
		return nil, fmt.Errorf("template inbound %d does not exist", templateID)
	}

	byHost := map[string]*Tunnel{}
	for _, t := range tunnels {
		byHost[t.Node.HostName] = t
	}

	used := n.store.usedPorts()
	created := []int{}
	for _, host := range hosts {
		t := byHost[host]
		if t == nil || t.Status != "up" {
			continue
		}
		port, err := freeRandomPort(used)
		if err != nil {
			return created, err
		}
		used[port] = true

		clone := &nativeInbound{
			ID:       n.store.NextID,
			Port:     port,
			Protocol: tpl.Protocol,
			Network:  tpl.Network,
			Path:     tpl.Path,
			Host:     tpl.Host,
			// Security must be cloned too; otherwise TLS/REALITY templates would silently become plaintext.
			Security: tpl.Security,
			TLS:      tpl.TLS,
			Reality:  tpl.Reality,
			Remark:   cloneRemark(tpl.Remark, exitLabel(t)),
			Enable:   true,
			Clients:  append([]nativeClient(nil), tpl.Clients...),
			BoundTo:  sanitizeTag(t.Node.HostName),
		}
		n.store.NextID++
		n.store.Inbounds = append(n.store.Inbounds, clone)
		created = append(created, port)
	}

	if len(created) == 0 {
		return created, fmt.Errorf("no available tunnels")
	}
	if err := n.apply(tunnels); err != nil {
		return created, err
	}
	return created, nil
}

func (n *Native) DeleteInbounds(ids []int, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	drop := map[int]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	kept := make([]*nativeInbound, 0, len(n.store.Inbounds))
	for _, ib := range n.store.Inbounds {
		if !drop[ib.ID] {
			kept = append(kept, ib)
		}
	}
	n.store.Inbounds = kept
	return n.apply(tunnels)
}

// UpdateInbound changes port, remark, or enabled state. Changing a port also
// changes inboundTag because the tag contains the port; full apply rewrites routing accordingly.
func (n *Native) UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("inbound %d does not exist", id)
	}

	if patch.Port != nil && *patch.Port != ib.Port {
		port := *patch.Port
		if port < 1 || port > 65535 {
			return fmt.Errorf("port %d is outside the valid range", port)
		}
		for _, other := range n.store.Inbounds {
			if other.ID != id && other.Port == port {
				return fmt.Errorf("port %d is already used by inbound %q", port, other.Remark)
			}
		}
		ib.Port = port
	}
	if patch.Remark != nil {
		if r := strings.TrimSpace(*patch.Remark); r != "" {
			ib.Remark = r
		}
	}
	if patch.Enable != nil {
		ib.Enable = *patch.Enable
	}
	return n.apply(tunnels)
}

// AddClient adds another client credential to an inbound.
func (n *Native) AddClient(id int, email string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("inbound %d does not exist", id)
	}

	email = strings.TrimSpace(email)
	if email == "" {
		email = fmt.Sprintf("%s-%d-%s", ib.Protocol, ib.Port, randomHex(3))
	}
	for _, c := range ib.Clients {
		if c.Email == email {
			return fmt.Errorf("client %q already exists", email)
		}
	}

	ib.Clients = append(ib.Clients, nativeClient{
		Email:    email,
		ID:       newUUID(),
		Password: randomHex(8),
		Enable:   true,
		Flow:     visionFlow(ib),
	})
	return n.apply(tunnels)
}

// DeleteClient removes one client. Keep at least one client so the inbound does
// not remain valid-but-unusable and appear broken.
func (n *Native) DeleteClient(id int, email string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("inbound %d does not exist", id)
	}
	if len(ib.Clients) <= 1 {
		return fmt.Errorf("this is the last client; deleting it would leave nobody able to connect")
	}

	kept := make([]nativeClient, 0, len(ib.Clients))
	for _, c := range ib.Clients {
		if c.Email != email {
			kept = append(kept, c)
		}
	}
	if len(kept) == len(ib.Clients) {
		return fmt.Errorf("client %q does not exist", email)
	}
	ib.Clients = kept
	return n.apply(tunnels)
}

// ResetClient generates new credentials so the old link stops working immediately.
func (n *Native) ResetClient(id int, email string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("inbound %d does not exist", id)
	}
	for i := range ib.Clients {
		if ib.Clients[i].Email == email {
			ib.Clients[i].ID = newUUID()
			ib.Clients[i].Password = randomHex(8)
			return n.apply(tunnels)
		}
	}
	return fmt.Errorf("client %q does not exist", email)
}

// visionFlow preserves the inbound's existing flow setting when adding a client.
func visionFlow(ib *nativeInbound) string {
	for _, c := range ib.Clients {
		if c.Flow != "" {
			return c.Flow
		}
	}
	return ""
}

// NewInboundSpec contains parameters for creating an inbound. Empty fields have
// sensible defaults: random port/path, automatic remark, and generated REALITY keys/shortId.
type NewInboundSpec struct {
	Protocol string
	Network  string
	Port     int
	Remark   string
	Path     string
	Host     string
	Security string
	// Vision enables xtls-rprx-vision for VLESS clients.
	Vision bool

	// TLS: an empty CertFile generates a self-signed certificate.
	ServerName string
	CertFile   string
	KeyFile    string

	// REALITY
	Dest        string
	ServerNames string // Comma-separated; empty derives the name from Dest.
	ShortID     string
	Fingerprint string
}

// nativeProtocols lists protocols supported by native mode and the frontend selector.
var nativeProtocols = map[string]bool{"vless": true, "vmess": true, "trojan": true}

// CreateInbound creates an inbound, allocating a random port when Port is zero.
func (n *Native) CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	ns, err := normalizeInboundSpec(spec, n.store.usedPorts())
	if err != nil {
		return nil, err
	}
	proto, network, security, port := ns.Protocol, ns.Network, ns.Security, ns.Port

	ib := &nativeInbound{
		ID:       n.store.NextID,
		Port:     port,
		Protocol: proto,
		Network:  network,
		Path:     ns.Path,
		Host:     ns.Host,
		Security: security,
		Remark:   ns.Remark,
		Enable:   true,
	}

	switch security {
	case "tls":
		conf, err := buildTLS(n.dir, spec)
		if err != nil {
			return nil, err
		}
		ib.TLS = conf
	case "reality":
		conf, err := buildReality(n.proc.bin, spec)
		if err != nil {
			return nil, err
		}
		ib.Reality = conf
	}

	ib.Clients = []nativeClient{{
		Email:    fmt.Sprintf("%s-%d", proto, port),
		ID:       newUUID(),
		Password: randomHex(8),
		Flow:     ns.Flow,
		Enable:   true,
	}}

	n.store.NextID++
	n.store.Inbounds = append(n.store.Inbounds, ib)

	if err := n.apply(tunnels); err != nil {
		// Roll back a bad inbound instead of leaving it persisted.
		n.store.Inbounds = n.store.Inbounds[:len(n.store.Inbounds)-1]
		n.store.NextID--
		_ = n.apply(tunnels)
		return nil, err
	}
	return &CreatedInbound{
		ID:       ib.ID,
		Port:     ib.Port,
		Protocol: ib.Protocol,
		Remark:   ib.Remark,
		Network:  ib.netOrTCP(),
		Security: ib.securityOrNone(),
	}, nil
}

// cloneRemark names a cloned inbound using the same rule as 3x-ui mode.
func cloneRemark(base, label string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return label
	}
	return base + "-" + label
}

// shareLink generates a client-importable share link.
func shareLink(ib *nativeInbound, c nativeClient, host string) string {
	net := ib.netOrTCP()
	sec := ib.securityOrNone()

	q := url.Values{}
	q.Set("type", net)
	q.Set("security", sec)

	switch net {
	case "ws", "httpupgrade", "xhttp":
		q.Set("path", ib.Path)
		if ib.Host != "" {
			q.Set("host", ib.Host)
		}
	case "grpc":
		q.Set("serviceName", strings.TrimPrefix(ib.Path, "/"))
	}

	switch sec {
	case "tls":
		if ib.TLS != nil {
			if ib.TLS.ServerName != "" {
				q.Set("sni", ib.TLS.ServerName)
			}
			// Self-signed certificates use certificate pinning in modern Xray clients.
			if ib.TLS.SelfSigned && ib.TLS.CertSha256 != "" {
				q.Set("pinSHA256", ib.TLS.CertSha256)
			}
		}
	case "reality":
		if ib.Reality != nil {
			if len(ib.Reality.ServerNames) > 0 {
				q.Set("sni", ib.Reality.ServerNames[0])
			}
			// pbk is the widely supported share-link field; newer Xray config uses password internally.
			q.Set("pbk", ib.Reality.PublicKey)
			if len(ib.Reality.ShortIDs) > 0 {
				q.Set("sid", ib.Reality.ShortIDs[0])
			}
			if ib.Reality.Fingerprint != "" {
				q.Set("fp", ib.Reality.Fingerprint)
			}
		}
	}

	if c.Flow != "" && ib.Protocol == "vless" {
		q.Set("flow", c.Flow)
	}

	frag := url.PathEscape(ib.Remark)

	switch ib.Protocol {
	case "trojan":
		return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", c.Password, host, ib.Port, q.Encode(), frag)
	case "vmess":
		// URI form is more consistently parsed across clients than VMess base64 variants.
		q.Set("encryption", "auto")
		return fmt.Sprintf("vmess://%s@%s:%d?%s#%s", c.ID, host, ib.Port, q.Encode(), frag)
	default:
		q.Set("encryption", "none")
		return fmt.Sprintf("vless://%s@%s:%d?%s#%s", c.ID, host, ib.Port, q.Encode(), frag)
	}
}

// buildTLS assembles TLS settings. If certificate paths are omitted, generate a self-signed certificate under dir/certs.
func buildTLS(dir string, spec NewInboundSpec) (*tlsConfig, error) {
	name := strings.TrimSpace(spec.ServerName)
	if name == "" {
		name = "localhost"
	}
	conf := &tlsConfig{ServerName: name}

	cert, key := strings.TrimSpace(spec.CertFile), strings.TrimSpace(spec.KeyFile)
	// Supplying only one path is likely a mistake; do not silently fall back to self-signed TLS.
	if (cert == "") != (key == "") {
		return nil, fmt.Errorf("certificate and private key must both be provided, or both left blank for a self-signed certificate")
	}
	if cert != "" && key != "" {
		if _, err := os.Stat(cert); err != nil {
			return nil, fmt.Errorf("certificate file is not readable: %w", err)
		}
		if _, err := os.Stat(key); err != nil {
			return nil, fmt.Errorf("private key file is not readable: %w", err)
		}
		conf.CertFile, conf.KeyFile = cert, key
		return conf, nil
	}

	c, k, err := selfSignedCert(dir, name)
	if err != nil {
		return nil, err
	}
	conf.CertFile, conf.KeyFile, conf.SelfSigned = c, k, true
	// The fingerprint is required for clients to trust the generated certificate.
	fp, err := certFingerprint(c)
	if err != nil {
		return nil, err
	}
	conf.CertSha256 = fp
	return conf, nil
}

// buildReality assembles REALITY settings and generates keys and shortId automatically.
func buildReality(xrayBin string, spec NewInboundSpec) (*realityConfig, error) {
	dest := strings.TrimSpace(spec.Dest)
	if dest == "" {
		// REALITY must complete a real TLS 1.3 handshake with dest, so use a stable default.
		dest = "www.tesla.com:443"
	}
	if !strings.Contains(dest, ":") {
		dest += ":443"
	}

	var names []string
	for _, s := range strings.Split(spec.ServerNames, ",") {
		if s = strings.TrimSpace(s); s != "" {
			names = append(names, s)
		}
	}
	if len(names) == 0 {
		// SNI should match the borrowed destination host.
		names = []string{strings.SplitN(dest, ":", 2)[0]}
	}

	priv, pub, err := realityKeys(xrayBin)
	if err != nil {
		return nil, err
	}
	if err := checkRealityDest(dest, names[0]); err != nil {
		return nil, fmt.Errorf("REALITY destination is unavailable; choose another dest: %w", err)
	}

	short := strings.TrimSpace(spec.ShortID)
	if short == "" {
		short = randomShortID()
	}
	fp := strings.TrimSpace(spec.Fingerprint)
	if fp == "" {
		fp = "chrome"
	}

	return &realityConfig{
		Dest:        dest,
		ServerNames: names,
		PrivateKey:  priv,
		PublicKey:   pub,
		ShortIDs:    []string{short},
		Fingerprint: fp,
	}, nil
}
