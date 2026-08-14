package main

import (
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const vpngateAPI = "https://www.vpngate.net/api/iphone/"

// vpngateMirror is a fallback used when the node list cannot be fetched
// directly. FANOUT_VPNGATE_MIRROR can point to a custom mirror; set it to an
// empty string to disable mirror fallback.
const vpngateMirror = "https://p.xy.kg/vpngate"

// mirrorKey only prevents casual crawler/port-scan abuse of the proxy; it is
// not intended as a security boundary.
const mirrorKey = "8rhIFzFKRJMFAe-xP5OQPclDEvSjKlHo"

func mirrorURL() string {
	if v, ok := os.LookupEnv("FANOUT_VPNGATE_MIRROR"); ok {
		return strings.TrimSpace(v)
	}
	return vpngateMirror
}

func mirrorAccessKey() string {
	if v, ok := os.LookupEnv("FANOUT_VPNGATE_MIRROR_KEY"); ok {
		return strings.TrimSpace(v)
	}
	return mirrorKey
}

// Node represents a VPN Gate node.
type Node struct {
	HostName    string  `json:"hostname"`
	IP          string  `json:"ip"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Ping        int     `json:"ping"`
	SpeedMbps   float64 `json:"speed_mbps"`
	Sessions    int     `json:"sessions"`
	Config      string  `json:"-"` // Decoded .ovpn content.
}

// fetchNodes fetches and parses the VPN Gate node list. It tries the direct API
// first, then the mirror if the request fails or returns invalid content. The
// returned list is sorted by speed in descending order.
func fetchNodes(timeout time.Duration) ([]Node, error) {
	return fetchNodesWith(vpngateAPI, timeout)
}

// fetchNodesWith accepts the direct URL as an argument so both paths are testable.
func fetchNodesWith(direct string, timeout time.Duration) ([]Node, error) {
	nodes, err := fetchNodesFrom(direct, "", timeout)
	if err == nil {
		return nodes, nil
	}
	mirror := mirrorURL()
	if mirror == "" {
		return nil, err
	}
	nodes, mirrorErr := fetchNodesFrom(mirror, mirrorAccessKey(), timeout)
	if mirrorErr != nil {
		return nil, fmt.Errorf("direct request failed (%v); mirror also failed: %w", err, mirrorErr)
	}
	return nodes, nil
}

func fetchNodesFrom(url, key string, timeout time.Duration) ([]Node, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch node list: %w", err)
	}
	if key != "" {
		req.Header.Set("X-Fanout-Key", key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch node list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch node list: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read node list: %w", err)
	}
	return parseNodeCSV(string(raw))
}

// parseNodeCSV parses VPN Gate CSV. The first line is "*vpn_servers", the
// second line is a header beginning with '#', and the final line is "*".
func parseNodeCSV(body string) ([]Node, error) {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		kept = append(kept, strings.TrimPrefix(line, "#"))
	}
	if len(kept) < 2 {
		return nil, fmt.Errorf("invalid node list format: not enough valid rows")
	}

	r := csv.NewReader(strings.NewReader(strings.Join(kept, "\n")))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse node CSV: %w", err)
	}

	header := records[0]
	idx := map[string]int{}
	for i, name := range header {
		idx[strings.TrimSpace(name)] = i
	}
	need := []string{"HostName", "IP", "CountryLong", "CountryShort", "Ping", "Speed", "OpenVPN_ConfigData_Base64"}
	for _, k := range need {
		if _, ok := idx[k]; !ok {
			return nil, fmt.Errorf("node list is missing field %s", k)
		}
	}

	var nodes []Node
	for _, rec := range records[1:] {
		get := func(k string) string {
			i := idx[k]
			if i >= len(rec) {
				return ""
			}
			return rec[i]
		}
		cfgB64 := get("OpenVPN_ConfigData_Base64")
		if cfgB64 == "" || get("HostName") == "" {
			continue
		}
		cfg, err := base64.StdEncoding.DecodeString(cfgB64)
		if err != nil {
			continue
		}
		ping, _ := strconv.Atoi(get("Ping"))
		speed, _ := strconv.ParseFloat(get("Speed"), 64)
		sessions, _ := strconv.Atoi(get("NumVpnSessions"))
		nodes = append(nodes, Node{
			HostName:    get("HostName"),
			IP:          get("IP"),
			Country:     get("CountryLong"),
			CountryCode: get("CountryShort"),
			Ping:        ping,
			SpeedMbps:   speed / 1e6,
			Sessions:    sessions,
			Config:      string(cfg),
		})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("node list is empty")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].SpeedMbps > nodes[j].SpeedMbps })
	return nodes, nil
}
