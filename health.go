package main

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	healthInterval = 10 * time.Second
	healthFailures = 2 // Require consecutive failures to avoid reacting to brief network instability.
	healthTimeout  = 6 * time.Second
)

// WatchHealth periodically checks whether each tunnel still reaches the
// internet through its VPN. VPN Gate nodes are volunteer-operated and can
// disappear at any time, so failed tunnels are automatically reconnected.
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

// tunnelHealthy verifies that a tunnel is still actually using the VPN.
//
// Connectivity alone is not enough: the network namespace can still reach the
// internet through host NAT after OpenVPN dies, but its exit IP then becomes
// the host IP. Compare the current exit IP with the one recorded at tunnel startup.
func (m *Manager) tunnelHealthy(t *Tunnel) bool {
	out, err := exec.Command("ip", "netns", "exec", t.nsName(),
		"curl", "-s", "--max-time", strconv.Itoa(int(healthTimeout.Seconds())),
		"http://api.ipify.org").Output()
	if err != nil {
		return false
	}
	got := strings.TrimSpace(string(out))
	if got == "" {
		return false
	}
	// A changed exit IP means the VPN is gone and traffic has fallen back to the host.
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
