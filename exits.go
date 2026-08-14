package main

import (
	"sync"
	"time"
)

// ExitInbound is a 3x-ui inbound attached to an exit.
type ExitInbound struct {
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	Remark   string `json:"remark"`
	Protocol string `json:"protocol"`
	Enable   bool   `json:"enable"`
	Tag      string `json:"tag"`
}

// Exit is one row in the UI: a tunnel plus every inbound routed through it.
// The user-facing unit is an exit, rather than separate tunnel and inbound concepts.
type Exit struct {
	Slot    int       `json:"slot"`
	Port    int       `json:"port"` // SOCKS5 port
	Host    string    `json:"host"`
	Region  string    `json:"region"`
	Country string    `json:"country"`
	ExitIP  string    `json:"exit_ip"`
	Status  string    `json:"status"`
	Err     string    `json:"err,omitempty"`
	Since   time.Time `json:"since"`
	// SOCKS5 credentials are shown, copied, and edited in the UI.
	SocksUser string        `json:"socks_user"`
	SocksPass string        `json:"socks_pass"`
	Inbounds  []ExitInbound `json:"inbounds"`
}

// ExitsView contains all data required by the main UI.
type ExitsView struct {
	Exits []Exit `json:"exits"`
	// Direct contains inbounds that are not bound to an exit. They remain visible
	// so users do not mistake them for deleted nodes.
	Direct []ExitInbound `json:"direct"`
	Panel  string        `json:"panel"` // Reason the panel is unavailable; empty when healthy.
	// Backend identifies the active node backend. The UI uses it to decide whether
	// inbound creation is available: managed panels own their inbounds, while native
	// mode lets fanout create them directly.
	Backend string `json:"backend"`
	// PanelInfo is a one-line backend description shown beside the title.
	PanelInfo string `json:"panel_info"`
	// PublicIP is the host's public IPv4, used as the SOCKS5/share-link connection address.
	PublicIP string `json:"public_ip"`
}

// inboundCache briefly caches the inbound list. The UI polls every few seconds,
// and each inbound read requires parsing the complete Xray configuration, so
// querying the panel on every poll would be wasteful.
type inboundCache struct {
	mu   sync.Mutex
	at   time.Time
	list []Inbound
	err  error
}

const inboundCacheTTL = 2500 * time.Millisecond

var ibCache inboundCache

func cachedInbounds(live map[string]bool) ([]Inbound, error) {
	ibCache.mu.Lock()
	defer ibCache.mu.Unlock()
	if time.Since(ibCache.at) < inboundCacheTTL {
		return ibCache.list, ibCache.err
	}

	var list []Inbound
	x, err := openPanel()
	if err == nil {
		list, err = x.Inbounds(live)
	}
	ibCache.at, ibCache.list, ibCache.err = time.Now(), list, err
	return list, err
}

// invalidateInbounds is called after writes so the next read reflects changes immediately.
func invalidateInbounds() {
	ibCache.mu.Lock()
	ibCache.at = time.Time{}
	ibCache.mu.Unlock()
}

// ExitsOf joins tunnels and inbounds into the shape consumed directly by the UI.
func (m *Manager) ExitsOf() ExitsView {
	tunnels := m.Tunnels()
	view := ExitsView{Exits: make([]Exit, 0, len(tunnels)), PublicIP: hostPublicIP()}

	// Populate backend type first so the UI still knows the active mode if reading inbounds fails.
	if p, err := openPanel(); err == nil {
		view.Backend = p.Kind()
		view.PanelInfo = p.Describe()
	}

	live := map[string]bool{}
	for _, t := range tunnels {
		if t.Status == "up" {
			live[sanitizeTag(t.Node.HostName)] = true
		}
	}

	byHost := map[string]int{}
	for i, t := range tunnels {
		byHost[sanitizeTag(t.Node.HostName)] = i
		cred := t.credential()
		view.Exits = append(view.Exits, Exit{
			Slot: t.Slot, Port: t.Port, Host: t.Node.HostName,
			Region: t.Node.CountryCode, Country: t.Node.Country,
			ExitIP: t.ExitIP, Status: t.Status, Err: t.Err, Since: t.Since,
			SocksUser: cred.User, SocksPass: cred.Pass,
		})
	}

	list, err := cachedInbounds(live)
	if err != nil {
		view.Panel = err.Error()
		return view
	}

	for _, ib := range list {
		row := ExitInbound{
			ID: ib.ID, Port: ib.Port, Remark: ib.Remark,
			Protocol: ib.Protocol, Enable: ib.Enable, Tag: ib.Tag,
		}
		if i, ok := byHost[ib.BoundTo]; ib.BoundTo != "" && ok {
			view.Exits[i].Inbounds = append(view.Exits[i].Inbounds, row)
			continue
		}
		view.Direct = append(view.Direct, row)
	}
	return view
}
