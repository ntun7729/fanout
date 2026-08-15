package main

import (
	"log"
	"time"
)

const (
	healthInterval = 10 * time.Second
	healthFailures = 2 // Require consecutive failures to avoid reacting to brief network instability.
	healthTimeout  = 6 * time.Second
)

// WatchHealth periodically checks whether each exit still reaches the internet
// through its selected VPN, proxy, or MASQUE transport. Failed exits automatically
// reconnect while preserving their local SOCKS5 port.
func (m *Manager) WatchHealth() {
	fails := map[int]int{}

	for range time.Tick(healthInterval) {
		for _, t := range m.Tunnels() {
			if t.Status != "up" {
				continue
			}
			if m.tunnelHealthy(t) {
				fails[t.Slot] = 0
				continue
			}

			fails[t.Slot]++
			if fails[t.Slot] < healthFailures {
				log.Printf("tunnel %d (%s) health check failed %d time(s)", t.Slot, t.Node.HostName, fails[t.Slot])
				continue
			}

			log.Printf("tunnel %d (%s) is offline; switching nodes and reconnecting", t.Slot, t.Node.HostName)
			fails[t.Slot] = 0
			m.reconnect(t, t.Node.HostName)
		}
	}
}

// tunnelHealthy verifies that the selected transport still produces a valid
// public exit. VPN Gate and public-proxy exits are expected to keep the same IP;
// WARP may rotate egress within the same healthy MASQUE session, so any valid
// WARP response is accepted and the displayed exit IP is refreshed.
func (m *Manager) tunnelHealthy(t *Tunnel) bool {
	got, err := t.probeExitIPWithTimeout(healthTimeout)
	if err != nil || got == "" {
		return false
	}
	if isMasqueNode(t.Node) {
		t.ExitIP = got
		return true
	}
	return got == t.ExitIP
}

// reconnect moves a tunnel to another node while keeping its slot and port so
// already distributed client configurations remain valid.
//
// oldHost must be the node name actually bound before this reconnect. If the
// caller has already changed t.Node, such as during a manual swap, it must pass
// the previous name so rebind can find and move the old inbound bindings.
func (m *Manager) reconnect(t *Tunnel, oldHost string) {
	t.Status = "starting"
	t.Err = "switching node and reconnecting"
	t.ExitIP = ""

	if t.ovpn != nil && t.ovpn.Process != nil {
		_ = t.ovpn.Process.Kill()
		t.ovpn = nil
	}
	// teardownNetns is also the common child-transport cleanup path and stops
	// a running usque process for WARP MASQUE exits.
	t.teardownNetns()

	go func() {
		// Notify only after rebind/resync. Those steps move inbounds to the new
		// node; rebuilding earlier could drop routing rules that still reference the old host.
		m.bringUpPersist(t, false, true)
		if t.Status != "up" {
			return
		}
		// Outbound tags follow node names. If the node changes, rebind inbounds
		// that pointed at the old outbound so panel routing does not reference a missing tag.
		if t.Node.HostName != oldHost {
			if err := m.rebind(oldHost, t); err != nil {
				log.Printf("failed to synchronize 3x-ui bindings after reconnect: %v", err)
			}
			return
		}
		// Even when the node name is unchanged, rewrite the outbound because the
		// exit IP may have changed and prior swaps may have left stale bindings.
		if err := m.resync(t); err != nil {
			log.Printf("failed to rewrite 3x-ui outbound after reconnect: %v", err)
		}
	}()
}
