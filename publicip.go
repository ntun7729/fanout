package main

import (
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// publicIPSources are endpoints that return a single plain IPv4 address. The
// first valid response is used.
var publicIPSources = []string{
	"https://api.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://ifconfig.me/ip",
}

var (
	publicIPMu       sync.Mutex
	publicIPOverride string    // Explicitly supplied by -ip / FANOUT_PUBLIC_IP; highest priority.
	publicIPCache    string    // Most recent successful detection result.
	publicIPAt       time.Time // Detection time used for TTL.
)

const publicIPTTL = 30 * time.Minute

// setPublicIPOverride records an explicitly supplied host public address. An
// empty value means no override.
func setPublicIPOverride(ip string) {
	publicIPMu.Lock()
	publicIPOverride = strings.TrimSpace(ip)
	publicIPMu.Unlock()
}

// hostPublicIP returns the public IPv4 address of the host running fanout.
// Explicit override wins, then a non-expired cache, then a fresh probe. If all
// probes fail, an empty string is returned unless an older cached value exists.
func hostPublicIP() string {
	publicIPMu.Lock()
	if publicIPOverride != "" {
		ip := publicIPOverride
		publicIPMu.Unlock()
		return ip
	}
	if publicIPCache != "" && time.Since(publicIPAt) < publicIPTTL {
		ip := publicIPCache
		publicIPMu.Unlock()
		return ip
	}
	publicIPMu.Unlock()

	ip := probePublicIP()
	if ip == "" {
		// On probe failure, prefer the last successful value over returning empty.
		publicIPMu.Lock()
		ip = publicIPCache
		publicIPMu.Unlock()
		return ip
	}

	publicIPMu.Lock()
	publicIPCache = ip
	publicIPAt = time.Now()
	publicIPMu.Unlock()
	return ip
}

// probePublicIP queries external endpoints and returns the first valid IPv4 response.
func probePublicIP() string {
	for _, url := range publicIPSources {
		out, err := exec.Command("curl", "-4", "-s", "--max-time", "5", url).Output()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(out))
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			return ip
		}
	}
	return ""
}
