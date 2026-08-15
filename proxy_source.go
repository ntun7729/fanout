package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	freeProxyListURL = "https://raw.githubusercontent.com/iplocate/free-proxy-list/main/all-proxies.txt"
	proxyNodeMarker  = "proxy:"
	proxyScanLimit   = 160
	proxyKeepLimit   = 30
	proxyWorkers     = 24
)

var proxyProbeTimeout = 5 * time.Second

// isProxyNode identifies nodes backed by a public upstream proxy rather than OpenVPN.
func isProxyNode(n Node) bool {
	return strings.HasPrefix(n.Config, proxyNodeMarker)
}

func proxyURLFromNode(n Node) string {
	return strings.TrimPrefix(n.Config, proxyNodeMarker)
}

// fetchFreeProxyNodes downloads IPLocate's frequently refreshed public list,
// independently verifies a bounded sample, and labels successful exits by country.
// The P- prefix keeps proxy regions separate from VPN Gate regions while allowing
// the existing region picker, swap logic, and persistence format to work unchanged.
func fetchFreeProxyNodes(timeout time.Duration) ([]Node, error) {
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
	if len(candidates) > proxyScanLimit {
		candidates = candidates[:proxyScanLimit]
	}

	type checked struct {
		raw     string
		exitIP  string
		latency time.Duration
	}
	jobs := make(chan string)
	results := make(chan checked, len(candidates))
	workers := proxyWorkers
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
				ip, err := probeProxyExit(raw, proxyProbeTimeout)
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

	working := make([]checked, 0, proxyKeepLimit)
	for r := range results {
		working = append(working, r)
	}
	if len(working) == 0 {
		return nil, fmt.Errorf("no working free proxies passed the live egress check")
	}
	sort.Slice(working, func(i, j int) bool { return working[i].latency < working[j].latency })
	if len(working) > proxyKeepLimit {
		working = working[:proxyKeepLimit]
	}

	type geo struct {
		code    string
		country string
	}
	geos := make([]geo, len(working))
	geoJobs := make(chan int)
	geoWorkers := 6
	if geoWorkers > len(working) {
		geoWorkers = len(working)
	}
	var geoWG sync.WaitGroup
	for i := 0; i < geoWorkers; i++ {
		geoWG.Add(1)
		go func() {
			defer geoWG.Done()
			for idx := range geoJobs {
				code, country, err := lookupProxyCountry(working[idx].exitIP, 4*time.Second)
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
	return nodes, nil
}

func parseProxyList(body string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(body, "\n") {
		raw := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.Port() == "" {
			continue
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks4", "socks5":
		default:
			continue
		}
		if _, err := strconv.Atoi(u.Port()); err != nil {
			continue
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, raw)
	}
	return out
}

func proxyNodeName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "proxy-unknown"
	}
	host := strings.NewReplacer(".", "-", ":", "-", "[", "", "]", "").Replace(u.Hostname())
	return "proxy-" + strings.ToLower(u.Scheme) + "-" + host + "-" + u.Port()
}

// probeProxyExit verifies that the proxy can carry a real HTTPS TCP connection
// and returns the observed public egress IP, rather than merely checking its port.
func probeProxyExit(raw string, timeout time.Duration) (string, error) {
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
	req, err := http.NewRequest(http.MethodGet, "https://api.iplocate.io/ip", nil)
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

func lookupProxyCountry(ip string, timeout time.Duration) (string, string, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet,
		"https://ipwho.is/"+url.PathEscape(ip)+"?fields=success,country,country_code", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "fanout/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("country lookup returned HTTP %d", resp.StatusCode)
	}
	var data struct {
		Success     bool   `json:"success"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&data); err != nil {
		return "", "", err
	}
	if !data.Success || data.CountryCode == "" {
		return "", "", fmt.Errorf("country lookup failed")
	}
	return data.CountryCode, data.Country, nil
}

// upstreamProxyDialer returns a TCP dialer that CONNECTs through HTTP/HTTPS,
// SOCKS4a, or SOCKS5. It intentionally supports the protocols present in
// IPLocate's all-proxies list so a fanout SOCKS5 port can relay arbitrary TCP.
func upstreamProxyDialer(raw string, timeout time.Duration) (func(network, addr string) (net.Conn, error), error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Hostname() == "" || u.Port() == "" {
		return nil, fmt.Errorf("invalid proxy URL %q", raw)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "https", "socks4", "socks5":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", scheme)
	}
	return func(network, addr string) (net.Conn, error) {
		if network != "tcp" && network != "tcp4" {
			return nil, fmt.Errorf("proxy exits support TCP only")
		}
		switch scheme {
		case "http", "https":
			return dialHTTPProxy(u, addr, timeout)
		case "socks4":
			return dialSOCKS4Proxy(u, addr, timeout)
		default:
			return dialSOCKS5Proxy(u, addr, timeout)
		}
	}, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func dialHTTPProxy(u *url.URL, target string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Conn, error) {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if strings.EqualFold(u.Scheme, "https") {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName:         u.Hostname(),
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // Public HTTPS proxy certificates are frequently self-signed; destination TLS is still verified separately.
		})
		if err := tlsConn.Handshake(); err != nil {
			return fail(err)
		}
		conn = tlsConn
	}

	var auth string
	if u.User != nil {
		pass, _ := u.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(u.User.Username() + ":" + pass))
		auth = "Proxy-Authorization: Basic " + token + "\r\n"
	}
	if _, err := fmt.Fprintf(conn,
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Connection: Keep-Alive\r\n%s\r\n",
		target, target, auth); err != nil {
		return fail(err)
	}
	r := bufio.NewReader(conn)
	status, err := r.ReadString('\n')
	if err != nil {
		return fail(err)
	}
	parts := strings.SplitN(strings.TrimSpace(status), " ", 3)
	if len(parts) < 2 {
		return fail(fmt.Errorf("invalid HTTP proxy response %q", strings.TrimSpace(status)))
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil || code/100 != 2 {
		return fail(fmt.Errorf("HTTP proxy CONNECT failed: %s", strings.TrimSpace(status)))
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return fail(err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return &bufferedConn{Conn: conn, r: r}, nil
}

func dialSOCKS5Proxy(u *url.URL, target string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Conn, error) {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

	methods := []byte{0x00}
	if u.User != nil {
		methods = append(methods, 0x02)
	}
	greet := append([]byte{0x05, byte(len(methods))}, methods...)
	if _, err := conn.Write(greet); err != nil {
		return fail(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fail(err)
	}
	if resp[0] != 0x05 || resp[1] == 0xff {
		return fail(fmt.Errorf("SOCKS5 proxy rejected authentication methods"))
	}
	if resp[1] == 0x02 {
		if u.User == nil {
			return fail(fmt.Errorf("SOCKS5 proxy requires credentials"))
		}
		user := u.User.Username()
		pass, _ := u.User.Password()
		if len(user) > 255 || len(pass) > 255 {
			return fail(fmt.Errorf("SOCKS5 proxy credentials are too long"))
		}
		msg := []byte{0x01, byte(len(user))}
		msg = append(msg, user...)
		msg = append(msg, byte(len(pass)))
		msg = append(msg, pass...)
		if _, err := conn.Write(msg); err != nil {
			return fail(err)
		}
		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil || authResp[1] != 0x00 {
			if err == nil {
				err = fmt.Errorf("SOCKS5 proxy authentication failed")
			}
			return fail(err)
		}
	}

	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return fail(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fail(fmt.Errorf("invalid target port"))
	}
	msg := []byte{0x05, 0x01, 0x00}
	ip := net.ParseIP(host)
	if v4 := ip.To4(); v4 != nil {
		msg = append(msg, 0x01)
		msg = append(msg, v4...)
	} else if v6 := ip.To16(); v6 != nil {
		msg = append(msg, 0x04)
		msg = append(msg, v6...)
	} else {
		if len(host) > 255 {
			return fail(fmt.Errorf("target hostname is too long"))
		}
		msg = append(msg, 0x03, byte(len(host)))
		msg = append(msg, host...)
	}
	pb := make([]byte, 2)
	binary.BigEndian.PutUint16(pb, uint16(port))
	msg = append(msg, pb...)
	if _, err := conn.Write(msg); err != nil {
		return fail(err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fail(err)
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		return fail(fmt.Errorf("SOCKS5 CONNECT failed with code %d", head[1]))
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return fail(err)
		}
		skip = int(l[0])
	default:
		return fail(fmt.Errorf("invalid SOCKS5 bind address type"))
	}
	if _, err := io.CopyN(io.Discard, conn, int64(skip+2)); err != nil {
		return fail(err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func dialSOCKS4Proxy(u *url.URL, target string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Conn, error) {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return fail(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fail(fmt.Errorf("invalid target port"))
	}
	msg := []byte{0x04, 0x01, 0, 0}
	binary.BigEndian.PutUint16(msg[2:4], uint16(port))
	ip := net.ParseIP(host).To4()
	use4a := ip == nil
	if use4a {
		msg = append(msg, 0, 0, 0, 1)
	} else {
		msg = append(msg, ip...)
	}
	user := ""
	if u.User != nil {
		user = u.User.Username()
	}
	msg = append(msg, user...)
	msg = append(msg, 0)
	if use4a {
		msg = append(msg, host...)
		msg = append(msg, 0)
	}
	if _, err := conn.Write(msg); err != nil {
		return fail(err)
	}
	resp := make([]byte, 8)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fail(err)
	}
	if resp[1] != 0x5a {
		return fail(fmt.Errorf("SOCKS4 CONNECT failed with code %d", resp[1]))
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
