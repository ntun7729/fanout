package main

import (
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
	proxyDiscoveryLimit   = 80
	proxyGeoWorkers       = 12
	proxyGeoLookupTimeout = 3 * time.Second
)

// fetchFreeProxyNodesV2 discovers proxy exits without requiring every proxy to
// pass a live relay test during the global node refresh. The upstream IPLocate
// list is already refreshed/tested frequently; fanout still performs its own
// live egress verification when an exit is actually started.
//
// This separation is important: a temporary block or timeout reaching the
// probe endpoint must not make the entire Free Proxy source disappear from the
// New Exit dialog. Country lookup is best-effort; unknown locations remain
// visible under P-XX instead of being discarded.
func fetchFreeProxyNodesV2(timeout time.Duration) ([]Node, error) {
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

	candidates := parseProxyList(string(blob))
	if len(candidates) == 0 {
		return nil, fmt.Errorf("free proxy list contained no supported proxies")
	}
	if len(candidates) > proxyDiscoveryLimit {
		candidates = candidates[:proxyDiscoveryLimit]
	}

	type discovered struct {
		raw     string
		ip      string
		code    string
		country string
	}

	jobs := make(chan int)
	found := make([]discovered, len(candidates))
	workers := proxyGeoWorkers
	if workers > len(candidates) {
		workers = len(candidates)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				raw := candidates[idx]
				u, err := url.Parse(raw)
				if err != nil {
					continue
				}
				host := u.Hostname()
				ip := proxyEndpointIP(host)
				code, country := "XX", "Unknown"
				if ip != "" {
					if c, n, err := lookupProxyCountry(ip, proxyGeoLookupTimeout); err == nil && c != "" {
						code = strings.ToUpper(c)
						country = n
					}
				}
				found[idx] = discovered{raw: raw, ip: ip, code: code, country: country}
			}
		}()
	}
	for i := range candidates {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	nodes := make([]Node, 0, len(found))
	for _, d := range found {
		if d.raw == "" {
			continue
		}
		u, err := url.Parse(d.raw)
		if err != nil {
			continue
		}
		ip := d.ip
		if ip == "" {
			ip = u.Hostname()
		}
		nodes = append(nodes, Node{
			HostName:    proxyNodeName(d.raw),
			IP:          ip,
			Country:     d.country,
			CountryCode: "P-" + d.code,
			Ping:        0,
			SpeedMbps:   0,
			Config:      proxyNodeMarker + d.raw,
		})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("free proxy list produced no usable proxy entries")
	}

	// Keep country groups stable in the UI while retaining multiple candidates
	// per country for automatic failover.
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].CountryCode != nodes[j].CountryCode {
			return nodes[i].CountryCode < nodes[j].CountryCode
		}
		return nodes[i].HostName < nodes[j].HostName
	})
	return nodes, nil
}

// proxyEndpointIP resolves the proxy server itself for country labeling. Most
// entries use literal IPv4 addresses, so this normally performs no DNS request.
func proxyEndpointIP(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return ""
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	if len(ips) > 0 {
		return ips[0].String()
	}
	return ""
}
