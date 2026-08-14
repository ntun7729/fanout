package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// persistedTunnel is the on-disk representation of a tunnel. Only information
// required for reconstruction is stored; runtime state such as namespaces,
// processes, and listeners is recreated after restart.
type persistedTunnel struct {
	Slot        int    `json:"slot"`
	Port        int    `json:"port"`
	HostName    string `json:"hostname"`
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	Config      string `json:"config"`
	// SOCKS5 credentials must persist because users may already have distributed them.
	SocksUser string `json:"socks_user,omitempty"`
	SocksPass string `json:"socks_pass,omitempty"`
}

type persistedState struct {
	Tunnels []persistedTunnel `json:"tunnels"`
}

func statePath(dir string) string { return filepath.Join(dir, "state.json") }

// saveState writes current tunnels to disk for restoration after restart.
func (m *Manager) saveState() error {
	var st persistedState
	for _, t := range m.Tunnels() {
		// Skip only tunnels explicitly stopped by the user. starting/failed tunnels
		// are retained because they may be reconnecting or waiting for another attempt.
		if t.Status == "stopped" {
			continue
		}
		st.Tunnels = append(st.Tunnels, persistedTunnel{
			Slot:        t.Slot,
			Port:        t.Port,
			HostName:    t.Node.HostName,
			CountryCode: t.Node.CountryCode,
			Country:     t.Node.Country,
			Config:      t.Node.Config,
			SocksUser:   t.Cred.User,
			SocksPass:   t.Cred.Pass,
		})
	}

	blob, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath(m.workDir) + ".tmp"
	if err := os.WriteFile(tmp, blob, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, statePath(m.workDir))
}

// restoreState reads saved tunnels and starts each one. Node configuration is
// persisted too, so a tunnel can be reconstructed even if that node has since
// disappeared from the current VPN Gate list.
func (m *Manager) restoreState() (int, error) {
	blob, err := os.ReadFile(statePath(m.workDir))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	var st persistedState
	if err := json.Unmarshal(blob, &st); err != nil {
		return 0, fmt.Errorf("failed to parse state file: %w", err)
	}

	// Fill region, latency, and other metadata from the current node list. If the
	// node is gone, fall back to the minimal information stored on disk.
	known := map[string]Node{}
	for _, n := range m.nodes {
		known[n.HostName] = n
	}

	for _, p := range st.Tunnels {
		node, ok := known[p.HostName]
		if !ok {
			// The node disappeared from VPN Gate; reconstruct it from persisted data.
			node = Node{
				HostName:    p.HostName,
				CountryCode: p.CountryCode,
				Country:     p.Country,
			}
		}
		node.Config = p.Config
		// State files from older versions have no credential fields, so generate a set.
		cred := SocksCred{User: p.SocksUser, Pass: p.SocksPass}
		if cred.User == "" || cred.Pass == "" {
			gen, err := newSocksCred()
			if err != nil {
				return 0, fmt.Errorf("failed to generate SOCKS5 credentials: %w", err)
			}
			cred = gen
		}
		t := &Tunnel{
			Slot:   p.Slot,
			Port:   p.Port,
			Node:   node,
			Status: "starting",
			Cred:   cred,
		}
		m.mu.Lock()
		m.tunnels[p.Slot] = t
		m.mu.Unlock()
		go m.bringUpPersist(t, true, true)
	}
	return len(st.Tunnels), nil
}
