package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	proxyReliableScanLimit = 240
	proxyReliableKeepLimit = 40
	proxyReliableWorkers   = 24
)

var proxyReliableProbeTimeout = 8 * time.Second

// fetchFreeProxyNodesV3 exposes only proxies that fanout has independently
// verified from this host. IPLocate tests several proxy protocols differently;
// fanout prefers SOCKS because SOCKS is suitable for arbitrary TCP relaying and
// requires HTTP/HTTPS proxies to support CONNECT before they are accepted.
func fetchFreeProxyNodesV3(timeout time.Duration) ([]Node, error) {
	listTimeout := timeout
	if listTimeout <= 0 || listTimeout > 15*time.Second {
		listTimeout = 15 * time.Second
	}
	client := &http.Client{Timeout: listTimeout}
	req, err := http.NewRequest(http.MethodGet, freeProxyListURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fanout/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch free proxy list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch free proxy list: HTTP %d", resp.StatusCode)
	}
	blob, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read free proxy list: %w", err)
	}

	candidates := prioritizeProxyCandidates(parseProxyList(string(blob)))
	if len(candidates) == 0 {
		return nil, fmt.Errorf("free proxy list contained no supported proxies")
	}
	if len(candidates) > proxyReliableScanLimit {
		candidates = candidates[:proxyReliableScanLimit]
	}

	type checked struct {
		raw     string
		exitIP  string
		latency time.Duration
	}
	jobs := make(chan string)
	results := make(chan checked, len(candidates))
	workers := proxyReliableWorkers
	if workers > len(candidates) {
		workers = len(candidates)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for raw := range jobs {
				started := time.Now()
				ip, err := probeProxyExitCompatible(raw, proxyReliableProbeTimeout)
				if err == nil {
					results <- checked{raw: raw, exitIP: ip, latency: time.Since(started)}
				}
			}
		}()
	}
	go func() {
		for _, raw := range candidates {
			jobs <- raw
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	working := make([]checked, 0, proxyReliableKeepLimit)
	for r := range results {
		working = append(working, r)
	}
	if len(working) == 0 {
		return nil, fmt.Errorf("no free proxies passed fanout's live relay check")
	}
	sort.SliceStable(working, func(i, j int) bool {
		pi, pj := proxyProtocolPriority(working[i].raw), proxyProtocolPriority(working[j].raw)
		if pi != pj {
			return pi < pj
		}
		return working[i].latency < working[j].latency
	})
	if len(working) > proxyReliableKeepLimit {
		working = working[:proxyReliableKeepLimit]
	}

	// Country lookup is intentionally performed only for proxies that already
	// passed the relay check. This keeps API usage low and prevents a geolocation
	// outage from determining whether a proxy is considered alive.
	type geo struct {
		code    string
		country string
	}
	geos := make([]geo, len(working))
	geoJobs := make(chan int)
	geoWorkers := 4
	if geoWorkers > len(working) {
		geoWorkers = len(working)
	}
	var geoWG sync.WaitGroup
	for i := 0; i < geoWorkers; i++ {
		geoWG.Add(1)
		go func() {
			defer geoWG.Done()
			for idx := range geoJobs {
				code, country, err := lookupProxyCountry(working[idx].exitIP, 5*time.Second)
				if err != nil || code == "" {
					code, country = "XX", "Unknown"
				}
				geos[idx] = geo{code: strings.ToUpper(code), country: country}
			}
		}()
	}
	for i := range working {
		geoJobs <- i
	}
	close(geoJobs)
	geoWG.Wait()

	nodes := make([]Node, 0, len(working))
	for i, r := range working {
		u, err := url.Parse(r.raw)
		if err != nil {
			continue
		}
		ping := int(r.latency.Milliseconds())
		if ping < 1 {
			ping = 1
		}
		nodes = append(nodes, Node{
			HostName:    proxyNodeName(r.raw),
			IP:          u.Hostname(),
			Country:     geos[i].country,
			CountryCode: "P-" + geos[i].code,
			Ping:        ping,
			SpeedMbps:   0,
			Config:      proxyNodeMarker + r.raw,
		})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("free proxy validation produced no usable exits")
	}
	return nodes, nil
}

// prioritizeProxyCandidates makes the bounded scan spend its budget on the
// transports most useful for a generic SOCKS5 exit. Plain HTTP proxies are last
// because many only forward HTTP requests and do not implement CONNECT.
func prioritizeProxyCandidates(in []string) []string {
	out := append([]string(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		return proxyProtocolPriority(out[i]) < proxyProtocolPriority(out[j])
	})
	return out
}

func proxyProtocolPriority(raw string) int {
	u, err := url.Parse(raw)
	if err != nil {
		return 9
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5":
		return 0
	case "socks4":
		return 1
	case "https":
		return 2
	case "http":
		return 3
	default:
		return 9
	}
}

func proxyProbeURL(raw string) string {
	u, err := url.Parse(raw)
	if err == nil {
		scheme := strings.ToLower(u.Scheme)
		// fanout's WARP MASQUE transport is exposed internally as a loopback
		// SOCKS5 listener. Probe that path with the same HTTPS endpoint that is
		// known to work through usque, rather than the HTTP probe used for public
		// SOCKS candidates. This also proves TLS-capable TCP relay before the WARP
		// exit is marked up.
		if scheme == "socks5" {
			if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLoopback() {
				return "https://api.ipify.org"
			}
		}
		switch scheme {
		case "socks4", "socks5":
			// Match IPLocate's own validation method for public SOCKS proxies. A
			// successful SOCKS CONNECT is already a generic TCP relay test.
			return "http://api.iplocate.io/ip"
		}
	}
	// HTTP-family proxies must prove CONNECT support, because fanout exposes them
	// as a generic SOCKS5 exit rather than as an HTTP-only forward proxy.
	return "https://api.iplocate.io/ip"
}

func probeProxyExitCompatible(raw string, timeout time.Duration) (string, error) {
	dial, err := upstreamProxyDialer(raw, timeout)
	if err != nil {
		return "", err
	}
	tr := &http.Transport{
		Proxy:               nil,
		TLSHandshakeTimeout: timeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dial(network, addr)
		},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Timeout: timeout, Transport: tr}
	req, err := http.NewRequest(http.MethodGet, proxyProbeURL(raw), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "fanout/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("egress probe returned HTTP %d", resp.StatusCode)
	}
	blob, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	ip := strings.Trim(strings.TrimSpace(string(blob)), "\"")
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("invalid egress IP response %q", ip)
	}
	return ip, nil
}
