package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SocksCred contains the SOCKS5 access credentials for one tunnel.
//
// Each tunnel has independent credentials so one leak does not expose every
// exit, and credentials can be reset for one tunnel without affecting others.
type SocksCred struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// Tunnel is one running exit: either an OpenVPN network namespace or an upstream
// public proxy, plus a local authenticated SOCKS5 port.
type Tunnel struct {
	Slot   int       `json:"slot"`
	Port   int       `json:"port"`
	Node   Node      `json:"node"`
	Status string    `json:"status"` // starting | up | failed | stopped
	ExitIP string    `json:"exit_ip"`
	Err    string    `json:"err,omitempty"`
	Since  time.Time `json:"since"`
	Cred   SocksCred `json:"cred"`

	ns       string
	listener net.Listener
	ovpn     *exec.Cmd
	mu       sync.Mutex
}

func (t *Tunnel) nsName() string { return fmt.Sprintf("fo%d", t.Slot) }
func (t *Tunnel) subnet() string { return fmt.Sprintf("10.99.%d", t.Slot) }

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runQuiet executes cleanup commands and ignores errors such as resources already being absent.
func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

// setupNetns creates the network namespace and veth link, then configures NAT and forwarding.
// Proxy exits do not need a namespace because their outbound path is selected per TCP connection.
func (t *Tunnel) setupNetns() error {
	if isProxyNode(t.Node) {
		t.teardownNetns()
		return nil
	}

	ns, sub := t.nsName(), t.subnet()
	veth, peer := fmt.Sprintf("fov%d", t.Slot), fmt.Sprintf("fop%d", t.Slot)

	t.teardownNetns()

	if err := run("ip", "netns", "add", ns); err != nil {
		return fmt.Errorf("failed to create network namespace; install full iproute2 and make sure the container permits network namespaces: %w", err)
	}
	if err := run("ip", "netns", "exec", ns, "ip", "link", "set", "lo", "up"); err != nil {
		return err
	}
	if err := run("ip", "link", "add", veth, "type", "veth", "peer", "name", peer); err != nil {
		return err
	}
	if err := run("ip", "link", "set", peer, "netns", ns); err != nil {
		return err
	}
	if err := run("ip", "addr", "add", sub+".1/30", "dev", veth); err != nil {
		return err
	}
	if err := run("ip", "link", "set", veth, "up"); err != nil {
		return err
	}
	if err := run("ip", "netns", "exec", ns, "ip", "addr", "add", sub+".2/30", "dev", peer); err != nil {
		return err
	}
	if err := run("ip", "netns", "exec", ns, "ip", "link", "set", peer, "up"); err != nil {
		return err
	}
	if err := run("ip", "netns", "exec", ns, "ip", "route", "add", "default", "via", sub+".1"); err != nil {
		return err
	}

	// DNS inside the namespace is used only by OpenVPN to resolve remote hostnames.
	nsDir := filepath.Join("/etc/netns", ns)
	if err := os.MkdirAll(nsDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", nsDir, err)
	}
	if err := os.WriteFile(filepath.Join(nsDir, "resolv.conf"), []byte("nameserver 8.8.8.8\n"), 0644); err != nil {
		return fmt.Errorf("failed to write resolv.conf: %w", err)
	}

	cidr := sub + ".0/30"
	ensureRule("nat", "POSTROUTING", "-s", cidr, "-j", "MASQUERADE")
	ensureRuleInsert("filter", "FORWARD", "-s", cidr, "-j", "ACCEPT")
	ensureRuleInsert("filter", "FORWARD", "-d", cidr, "-j", "ACCEPT")
	return nil
}

// ensureRule idempotently appends an iptables rule.
func ensureRule(table, chain string, spec ...string) {
	check := append([]string{"-w", "5", "-t", table, "-C", chain}, spec...)
	if exec.Command("iptables", check...).Run() == nil {
		return
	}
	add := append([]string{"-w", "5", "-t", table, "-A", chain}, spec...)
	runQuiet("iptables", add...)
}

// ensureRuleInsert idempotently inserts a rule at the start of a chain. FORWARD
// chains often end in a catch-all REJECT, so fanout rules must be inserted first.
func ensureRuleInsert(table, chain string, spec ...string) {
	check := append([]string{"-w", "5", "-t", table, "-C", chain}, spec...)
	if exec.Command("iptables", check...).Run() == nil {
		return
	}
	ins := append([]string{"-w", "5", "-t", table, "-I", chain, "1"}, spec...)
	runQuiet("iptables", ins...)
}

func (t *Tunnel) teardownNetns() {
	ns, sub := t.nsName(), t.subnet()
	cidr := sub + ".0/30"
	runQuiet("ip", "netns", "del", ns)
	runQuiet("ip", "link", "del", fmt.Sprintf("fov%d", t.Slot))
	runQuiet("iptables", "-w", "5", "-t", "nat", "-D", "POSTROUTING", "-s", cidr, "-j", "MASQUERADE")
	runQuiet("iptables", "-w", "5", "-D", "FORWARD", "-s", cidr, "-j", "ACCEPT")
	runQuiet("iptables", "-w", "5", "-D", "FORWARD", "-d", cidr, "-j", "ACCEPT")
}

// startOpenVPN starts OpenVPN inside the namespace and waits for tun0 to receive an address.
// Proxy exits have no OpenVPN process; their live check happens in probeExitIP.
func (t *Tunnel) startOpenVPN(dir string) error {
	if isProxyNode(t.Node) {
		return nil
	}

	ns := t.nsName()
	cfgPath := filepath.Join(dir, ns+".ovpn")
	if err := os.WriteFile(cfgPath, []byte(t.Node.Config), 0600); err != nil {
		return fmt.Errorf("failed to write configuration: %w", err)
	}
	authPath := filepath.Join(dir, "auth.txt")
	if err := os.WriteFile(authPath, []byte("vpn\nvpn\n"), 0600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}

	logPath := filepath.Join(dir, ns+".log")
	cmd := exec.Command("ip", "netns", "exec", ns, "openvpn",
		"--config", cfgPath,
		"--auth-user-pass", authPath,
		"--auth-nocache",
		"--dev", "tun0",
		"--connect-retry-max", "2",
		"--connect-timeout", "20",
		"--data-ciphers", "AES-128-CBC:AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305",
		"--verb", "3",
		"--log", logPath,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start OpenVPN: %w", err)
	}
	t.ovpn = cmd
	go cmd.Wait() // Reap the child process to avoid zombies.

	// SOCKS5 cannot route through the VPN until OpenVPN has created tun0.
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if out, err := exec.Command("ip", "netns", "exec", ns, "ip", "-4", "addr", "show", "tun0").Output(); err == nil {
			if strings.Contains(string(out), "inet ") {
				return nil
			}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return fmt.Errorf("OpenVPN exited early; see %s", logPath)
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timed out waiting for tun0; see %s", logPath)
}

// serve listens for SOCKS5 on the host. Each connection resolves its outbound
// dialer at accept time so automatic swaps can switch between proxy endpoints
// without rebinding the public SOCKS5 listener.
func (t *Tunnel) serve() error {
	// Keep the assigned port whenever possible so distributed client configuration remains valid.
	var ln net.Listener
	var err error
	for i := 0; i < 6; i++ {
		ln, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", t.Port))
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		// Only switch ports when another process is genuinely holding the old one.
		port, perr := freeRandomPort(map[int]bool{t.Port: true})
		if perr != nil {
			return fmt.Errorf("failed to listen on %d and no fallback port is available: %w", t.Port, err)
		}
		ln, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
		if err != nil {
			return fmt.Errorf("failed to listen on %d: %w", port, err)
		}
		t.Port = port
	}
	t.listener = ln

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Read credentials per connection so changes apply immediately without rebinding the listener.
			cred := t.credential()
			go serveSocks(conn, &cred, t.dialOutbound)
		}
	}()
	return nil
}

// dialOutbound selects either the current upstream public proxy or the tunnel's
// network namespace. Reading t.Node per connection makes proxy swaps immediate.
func (t *Tunnel) dialOutbound(network, addr string) (net.Conn, error) {
	if isProxyNode(t.Node) {
		dial, err := upstreamProxyDialer(proxyURLFromNode(t.Node), 20*time.Second)
		if err != nil {
			return nil, err
		}
		return dial(network, addr)
	}
	return dialerInNetns(t.nsName())(network, addr)
}

// credential returns a credentials copy while avoiding concurrent reads/writes.
func (t *Tunnel) credential() SocksCred {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Cred
}

// setCredential replaces this tunnel's SOCKS5 credentials. Existing sessions
// are unaffected; new connections use the new credentials immediately.
func (t *Tunnel) setCredential(c SocksCred) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Cred = c
}

// probeExitIP queries the actual public IP through the selected exit transport.
func (t *Tunnel) probeExitIP() (string, error) {
	return t.probeExitIPWithTimeout(15 * time.Second)
}

func (t *Tunnel) probeExitIPWithTimeout(timeout time.Duration) (string, error) {
	if isProxyNode(t.Node) {
		ip, err := probeProxyExitCompatible(proxyURLFromNode(t.Node), timeout)
		if err != nil {
			return "", fmt.Errorf("failed to query proxy exit IP: %w", err)
		}
		return ip, nil
	}

	seconds := int(timeout.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	out, err := exec.Command("ip", "netns", "exec", t.nsName(),
		"curl", "-s", "--max-time", strconv.Itoa(seconds), "http://api.ipify.org").Output()
	if err != nil {
		return "", fmt.Errorf("failed to query exit IP: %w", err)
	}
	ip := strings.TrimSpace(string(out))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("invalid exit IP response: %q", ip)
	}
	return ip, nil
}

// stop stops this tunnel and releases all of its runtime resources.
func (t *Tunnel) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener != nil {
		t.listener.Close()
		t.listener = nil
	}
	if t.ovpn != nil && t.ovpn.Process != nil {
		_ = t.ovpn.Process.Kill()
		t.ovpn = nil
	}
	t.teardownNetns()
	t.Status = "stopped"
}
