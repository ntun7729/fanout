package main

import (
	"fmt"
	"log"
	"os/exec"
	"sort"
	"sync"
	"time"
)

// Manager maintains all tunnels and allocates slots and ports.
type Manager struct {
	mu       sync.RWMutex
	tunnels  map[int]*Tunnel
	nodes    []Node
	fetched  time.Time
	workDir  string
	maxSlots int
	jobs     JobStore
}

func NewManager(maxSlots int, workDir string) *Manager {
	return &Manager{
		tunnels:  map[int]*Tunnel{},
		workDir:  workDir,
		maxSlots: maxSlots,
	}
}

// RefreshNodes fetches the VPN Gate node list again.
func (m *Manager) RefreshNodes() (int, error) {
	nodes, err := fetchNodes(60 * time.Second)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.nodes = nodes
	m.fetched = time.Now()
	m.mu.Unlock()
	return len(nodes), nil
}

func (m *Manager) Nodes() ([]Node, time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Node, len(m.nodes))
	copy(out, m.nodes)
	return out, m.fetched
}

func (m *Manager) Tunnels() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// freeSlot finds an unused slot. The slot also determines the tunnel subnet.
func (m *Manager) freeSlot() (int, error) {
	for i := 1; i <= m.maxSlots; i++ {
		if _, used := m.tunnels[i]; !used {
			return i, nil
		}
	}
	return 0, fmt.Errorf("all slots are in use (limit %d)", m.maxSlots)
}

// Start creates a tunnel for the specified node and assigns a local SOCKS5 port.
func (m *Manager) Start(node Node) (*Tunnel, error) {
	m.mu.Lock()
	slot, err := m.freeSlot()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	// Choose a random port to avoid predictable collisions with other services.
	taken := map[int]bool{}
	for _, other := range m.tunnels {
		taken[other.Port] = true
	}
	port, err := freeRandomPort(taken)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	cred, err := newSocksCred()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	t := &Tunnel{
		Slot:   slot,
		Port:   port,
		Node:   node,
		Status: "starting",
		Since:  time.Now(),
		Cred:   cred,
	}
	m.tunnels[slot] = t
	m.mu.Unlock()

	go m.bringUp(t, true)
	return t, nil
}

// bringUp starts a tunnel.
//
// notify controls whether backend configuration is rebuilt immediately after
// success. Reconnect paths pass false because they subsequently rebind/resync
// inbounds to the new node; rebuilding earlier could lose routes still pointing
// at the old node name.
func (m *Manager) bringUp(t *Tunnel, notify bool) {
	m.bringUpPersist(t, notify, false)
}

// Backoff for automatic reconnects after an entire candidate round fails.
const (
	reconnectBackoffMin = 5 * time.Second
	reconnectBackoffMax = 60 * time.Second
)

// bringUpPersist starts a tunnel.
//
// persist=false (manual creation): try one candidate round, then mark failed so
// the user can see and retry immediately.
// persist=true (automatic reconnect / startup restore): keep retrying after a
// backoff and node-list refresh until the tunnel connects or the user stops it.
// VPN Gate contains many transient/dead volunteer nodes, so one failed round
// should not permanently kill an existing exit.
func (m *Manager) bringUpPersist(t *Tunnel, notify bool, persist bool) {
	backoff := reconnectBackoffMin
	for {
		if m.tryCandidates(t, notify) {
			return
		}
		// Stop retrying if the tunnel was removed or explicitly stopped.
		if !persist || !m.tunnelActive(t) {
			if persist {
				return
			}
			t.Status = "failed"
			if serr := m.saveState(); serr != nil {
				log.Printf("failed to save state: %v", serr)
			}
			return
		}

		t.Status = "starting"
		t.Err = fmt.Sprintf("no available node; retrying in %.0f seconds", backoff.Seconds())
		log.Printf("tunnel %d: all candidates failed; refreshing nodes and retrying in %.0f seconds", t.Slot, backoff.Seconds())
		time.Sleep(backoff)
		if !m.tunnelActive(t) {
			return
		}
		if _, err := m.RefreshNodes(); err != nil {
			log.Printf("failed to refresh node list before retry: %v", err)
		}
		if backoff < reconnectBackoffMax {
			backoff *= 2
			if backoff > reconnectBackoffMax {
				backoff = reconnectBackoffMax
			}
		}
	}
}

// tryCandidates tries one round of candidate nodes and returns true on success.
// Failure leaves Status unchanged for the caller to decide.
func (m *Manager) tryCandidates(t *Tunnel, notify bool) bool {
	// VPN Gate is volunteer-operated and many listed nodes are offline or full.
	// Automatically try the next candidate instead of forcing manual retries.
	candidates := m.candidatesFor(t.Node)
	for i, node := range candidates {
		if !m.tunnelActive(t) {
			return false
		}
		// Another tunnel may have taken this node while we were retrying.
		if i > 0 && m.nodeInUse(node.HostName, t.Slot) {
			continue
		}
		t.Node = node
		t.Status = "starting"
		if i > 0 {
			t.Err = fmt.Sprintf("switched to candidate node %d", i+1)
		}

		err := m.tryNode(t)
		if err == nil {
			t.Status = "up"
			t.Err = ""
			if serr := m.saveState(); serr != nil {
				log.Printf("failed to save state: %v", serr)
			}
			if notify {
				m.notifyPanel()
			}
			return true
		}
		t.teardownNetns()
	}
	return false
}

// tunnelActive reports whether this exact tunnel is still managed and not stopped.
func (m *Manager) tunnelActive(t *Tunnel) bool {
	if t.Status == "stopped" {
		return false
	}
	m.mu.RLock()
	cur, ok := m.tunnels[t.Slot]
	m.mu.RUnlock()
	return ok && cur == t
}

// tryNode attempts to start the tunnel using its current node.
func (m *Manager) tryNode(t *Tunnel) error {
	if err := t.setupNetns(); err != nil {
		return err
	}
	if err := t.startOpenVPN(m.workDir); err != nil {
		return err
	}
	if t.listener == nil {
		if err := t.serve(); err != nil {
			return err
		}
	}
	ip, err := t.probeExitIP()
	if err != nil {
		return err
	}
	t.ExitIP = ip
	return nil
}

// candidatesFor returns the requested node first, followed by alternatives from the same region.
func (m *Manager) candidatesFor(first Node) []Node {
	const maxTries = 6
	m.mu.RLock()
	defer m.mu.RUnlock()

	used := map[string]bool{first.HostName: true}
	for _, t := range m.tunnels {
		used[t.Node.HostName] = true
	}

	// Determine the candidate region. If missing, recover it from the current list
	// so an empty region does not accidentally make every node equivalent.
	region := first.CountryCode
	if region == "" {
		for _, n := range m.nodes {
			if n.HostName == first.HostName {
				region = n.CountryCode
				break
			}
		}
	}

	out := []Node{first}
	for _, n := range m.nodes {
		if len(out) >= maxTries {
			break
		}
		if used[n.HostName] {
			continue
		}
		// If region cannot be determined, allow any alternative rather than fail completely.
		if region != "" && n.CountryCode != region {
			continue
		}
		out = append(out, n)
	}
	return out
}

// Stop stops a tunnel and releases its slot.
func (m *Manager) Stop(slot int) error {
	invalidateInbounds()
	m.mu.Lock()
	t, ok := m.tunnels[slot]
	if ok {
		delete(m.tunnels, slot)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("slot %d has no running tunnel", slot)
	}
	t.stop()
	if err := m.saveState(); err != nil {
		log.Printf("failed to save state: %v", err)
	}
	m.notifyPanel()
	return nil
}

// Swap moves a tunnel to a different node in the same region while preserving
// its port and already distributed client configuration.
func (m *Manager) Swap(slot int) error {
	m.mu.RLock()
	t, ok := m.tunnels[slot]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("slot %d has no running tunnel", slot)
	}
	if t.Status == "starting" {
		return fmt.Errorf("this exit is still connecting; please wait")
	}

	// pickNodes excludes all nodes already in use, including the current one.
	picks, err := m.pickNodes(t.Node.CountryCode, 1)
	if err != nil {
		return err
	}
	oldHost := t.Node.HostName
	t.Node = picks[0]
	m.reconnect(t, oldHost)
	return nil
}

// StopAll stops every tunnel and clears persisted state.
func (m *Manager) StopAll() {
	for _, t := range m.Tunnels() {
		_ = m.Stop(t.Slot)
	}
}

// SetCred changes SOCKS5 credentials for an exit. Empty user and password
// means generate a new random pair.
//
// The backend must be notified because local Xray SOCKS outbounds include these
// credentials. Without synchronization, panel-managed nodes could no longer
// connect to their own exit.
func (m *Manager) SetCred(slot int, cred SocksCred) (SocksCred, error) {
	m.mu.RLock()
	t, ok := m.tunnels[slot]
	m.mu.RUnlock()
	if !ok {
		return SocksCred{}, fmt.Errorf("slot %d has no running tunnel", slot)
	}

	if cred.User == "" && cred.Pass == "" {
		gen, err := newSocksCred()
		if err != nil {
			return SocksCred{}, err
		}
		cred = gen
	}
	if err := validateCred(cred); err != nil {
		return SocksCred{}, err
	}

	t.setCredential(cred)
	if err := m.saveState(); err != nil {
		log.Printf("failed to save state: %v", err)
	}
	m.syncCred(t)
	return cred, nil
}

// ReconcileOutbounds aligns backend outbounds with restored tunnels, including
// SOCKS5 credentials. This matters for 3x-ui because its OnTunnelsChanged is a
// no-op and older persisted panel outbounds may lack authentication fields.
func (m *Manager) ReconcileOutbounds() {
	p, err := openPanel()
	if err != nil || p.Kind() != "3x-ui" {
		return
	}

	// Wait for tunnels to settle so the rewrite includes as many as possible.
	deadline := time.Now().Add(90 * time.Second)
	for {
		tunnels := m.Tunnels()
		if len(tunnels) == 0 {
			return
		}
		var up *Tunnel
		settled := true
		for _, t := range tunnels {
			if t.Status == "up" && up == nil {
				up = t
			}
			if t.Status == "starting" {
				settled = false
			}
		}
		if (settled || time.Now().After(deadline)) && up != nil {
			if err := m.resync(up); err != nil {
				log.Printf("failed to reconcile panel outbounds at startup: %v", err)
			}
			return
		}
		if settled || time.Now().After(deadline) {
			return // All tunnels failed; there is no outbound to rewrite.
		}
		time.Sleep(2 * time.Second)
	}
}

// syncCred writes updated credentials into the backend SOCKS outbound. Both
// backends expose this through ResyncOutbound even though their implementations differ.
func (m *Manager) syncCred(t *Tunnel) {
	if err := m.resync(t); err != nil {
		log.Printf("failed to synchronize SOCKS5 credentials to node-link backend: %v", err)
	}
}

// Shutdown stops runtime resources but preserves state for restoration next start.
func (m *Manager) Shutdown() {
	for _, t := range m.Tunnels() {
		t.stop()
	}
}

// prepareHost enables IP forwarding required by network namespace egress.
func prepareHost() error {
	if err := exec.Command("sysctl", "-qw", "net.ipv4.ip_forward=1").Run(); err != nil {
		return fmt.Errorf("failed to enable ip_forward: %w", err)
	}
	return nil
}

// nodeInUse reports whether another tunnel already uses a node.
func (m *Manager) nodeInUse(host string, exceptSlot int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for slot, t := range m.tunnels {
		if slot != exceptSlot && t.Node.HostName == host {
			return true
		}
	}
	return false
}

// rebind moves inbounds from an old node to a new node after a tunnel swap.
// If the backend is unavailable, health/reconnect should continue without failing.
func (m *Manager) rebind(oldHost string, t *Tunnel) error {
	x, err := openPanel()
	if err != nil {
		return nil
	}
	return x.Rebind(oldHost, t, m.Tunnels())
}

// resync rewrites an outbound after reconnecting without changing the node name.
func (m *Manager) resync(t *Tunnel) error {
	x, err := openPanel()
	if err != nil {
		return nil
	}
	return x.ResyncOutbound(t, m.Tunnels())
}

// notifyPanel tells the backend that the tunnel set changed. Native mode derives
// outbounds from the current tunnel list, while 3x-ui treats this as a no-op.
// Backend errors are logged rather than making exit creation/removal fail.
func (m *Manager) notifyPanel() {
	p, err := openPanel()
	if err != nil {
		return
	}
	if err := p.OnTunnelsChanged(m.Tunnels()); err != nil {
		log.Printf("failed to synchronize node-link backend: %v", err)
	}
}
