package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Panel is fanout's node-link backend.
//
// Implementations include XUI for an installed 3x-ui panel and Native for
// fanout-managed Xray. The UI and orchestration layer depend only on this
// interface so operations behave consistently across backends.
type Panel interface {
	// Kind returns "3x-ui", "native", or another backend identifier used by the UI.
	Kind() string
	// Describe returns a short human-readable backend description.
	Describe() string

	Inbounds(live map[string]bool) ([]Inbound, error)
	InboundDetail(id int, publicHost string) (*InboundDetail, error)
	InboundLinks(ids []int, publicHost string) ([]string, error)

	Bind(inboundTag string, hostname string, tunnels []*Tunnel) error
	Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error
	ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error

	CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error)
	DeleteInbounds(ids []int, tunnels []*Tunnel) error

	// CreateInbound creates an inbound. Native mode writes its own store and
	// rebuilds Xray; 3x-ui uses the panel's inbounds/add API.
	CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error)

	// UpdateInbound changes port, remark, or enabled state. Nil fields are left unchanged.
	UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error

	// AddClient adds a client; an empty email is named automatically.
	AddClient(id int, email string, tunnels []*Tunnel) error
	// DeleteClient removes one client from an inbound.
	DeleteClient(id int, email string, tunnels []*Tunnel) error
	// ResetClient replaces client credentials, immediately invalidating old links.
	ResetClient(id int, email string, tunnels []*Tunnel) error

	// OnTunnelsChanged runs after the tunnel set changes. Native mode derives its
	// outbounds from tunnels and must rebuild; 3x-ui synchronizes during Bind/Clone
	// and treats this as a no-op to avoid unnecessary Xray restarts.
	OnTunnelsChanged(tunnels []*Tunnel) error

	// Close releases backend resources. Native mode must stop its own Xray process.
	Close()
}

// InboundPatch describes a partial inbound update. Nil means leave the field unchanged.
type InboundPatch struct {
	Port   *int
	Remark *string
	Enable *bool
}

// CreatedInbound is the summary returned to the UI after creating an inbound.
type CreatedInbound struct {
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Remark   string `json:"remark"`
	Network  string `json:"network"`
	Security string `json:"security"`
}

// closePanel releases backend resources during process shutdown.
func closePanel() {
	panelState.mu.Lock()
	p := panelState.current
	panelState.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

// panelState caches the selected backend. Detection may execute x-ui commands,
// so it should not run for every request.
var panelState struct {
	mu      sync.Mutex
	current Panel
	workDir string
	forced  string
}

// panelModeFile stores the backend selected in the UI. The -panel flag has higher priority.
func panelModeFile(dir string) string { return filepath.Join(dir, "panel_mode") }

// configurePanel records the working directory and requested mode. Empty mode
// loads the saved UI choice, then falls back to automatic detection.
func configurePanel(workDir, mode string) {
	panelState.mu.Lock()
	defer panelState.mu.Unlock()
	panelState.workDir = workDir
	if mode == "" {
		blob, err := os.ReadFile(panelModeFile(workDir))
		if err == nil {
			mode = strings.TrimSpace(string(blob))
		}
	}
	panelState.forced = mode
	panelState.current = nil
}

// savePanelMode persists a UI backend choice. Empty mode removes the saved choice and restores auto-detection.
func savePanelMode(dir, mode string) error {
	if dir == "" {
		return nil
	}
	path := panelModeFile(dir)
	if mode == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte(mode), 0600)
}

// openPanel returns the currently available backend.
//
// Automatic mode prefers installed node managers to avoid launching another
// Xray instance that could collide with their inbound ports.
func openPanel() (Panel, error) {
	panelState.mu.Lock()
	defer panelState.mu.Unlock()

	if panelState.current != nil {
		return panelState.current, nil
	}

	switch panelState.forced {
	case "3x-ui":
		x, err := DetectXUI(panelState.workDir)
		if err != nil {
			return nil, fmt.Errorf("3x-ui mode was selected but detection failed: %w", err)
		}
		panelState.current = x
		return x, nil
	case "native":
		n, err := openNative(panelState.workDir)
		if err != nil {
			return nil, err
		}
		panelState.current = n
		return n, nil
	case "xray-cf-lite":
		xc, err := DetectXCL()
		if err != nil {
			return nil, fmt.Errorf("xray-cf-lite mode was selected but detection failed: %w", err)
		}
		panelState.current = xc
		return xc, nil
	}

	if xc, err := DetectXCL(); err == nil {
		panelState.current = xc
		return xc, nil
	}

	if x, err := DetectXUI(panelState.workDir); err == nil {
		panelState.current = x
		return x, nil
	} else if !xuiAbsent() {
		// If 3x-ui is installed but unreadable, do not start native Xray and risk port collisions.
		return nil, fmt.Errorf("3x-ui was detected but its configuration could not be read: %w", err)
	}

	n, err := openNative(panelState.workDir)
	if err != nil {
		return nil, err
	}
	panelState.current = n
	return n, nil
}

// currentPanelMode returns the effective backend mode.
func currentPanelMode() string {
	panelState.mu.Lock()
	defer panelState.mu.Unlock()
	if panelState.forced != "" {
		return panelState.forced
	}
	if panelState.current != nil {
		return panelState.current.Kind()
	}
	return ""
}

// availablePanelModes detects which backends are available for the Settings UI.
func availablePanelModes(workDir string) []map[string]any {
	modes := []map[string]any{}

	xcOK, xcReason := true, ""
	if _, err := DetectXCL(); err != nil {
		xcOK, xcReason = false, err.Error()
	}
	modes = append(modes, map[string]any{"mode": "xray-cf-lite", "label": "xray-cf-lite", "available": xcOK, "reason": xcReason})

	xuiOK, xuiReason := true, ""
	if _, err := DetectXUI(workDir); err != nil {
		xuiOK, xuiReason = false, err.Error()
	}
	modes = append(modes, map[string]any{"mode": "3x-ui", "label": "3x-ui panel", "available": xuiOK, "reason": xuiReason})

	// Native mode is generally available; exact Xray binary validation occurs on switch.
	modes = append(modes, map[string]any{"mode": "native", "label": "Built-in Xray", "available": true, "reason": ""})

	return modes
}

// switchPanelMode switches backends at runtime. Empty mode restores automatic detection.
// The old backend is closed first; if the requested backend fails, selection rolls
// back to automatic mode so fanout is not stuck on an unusable backend.
func switchPanelMode(mode string) (Panel, error) {
	switch mode {
	case "", "3x-ui", "native", "xray-cf-lite":
	default:
		return nil, fmt.Errorf("unknown backend mode %q", mode)
	}

	panelState.mu.Lock()
	old := panelState.current
	workDir := panelState.workDir
	panelState.mu.Unlock()
	if old != nil {
		old.Close()
	}

	panelState.mu.Lock()
	panelState.forced = mode
	panelState.current = nil
	panelState.mu.Unlock()

	p, err := openPanel()
	if err != nil {
		// Roll back to automatic detection instead of trapping the user in a broken mode.
		panelState.mu.Lock()
		panelState.forced = ""
		panelState.current = nil
		panelState.mu.Unlock()
		return nil, err
	}
	if err := savePanelMode(workDir, mode); err != nil {
		log.Printf("failed to persist backend mode (the current switch is still active): %v", err)
	}
	return p, nil
}

// xuiAbsent reports whether 3x-ui is not installed at all.
func xuiAbsent() bool {
	if _, err := os.Stat(xuiBinary); err == nil {
		return false
	}
	if _, err := os.Stat(xuiMenu); err == nil {
		return false
	}
	return true
}
