package main

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// credAlphabet avoids easily confused characters and symbols that require
// escaping in socks5:// URLs.
const credAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Username/password length limits. RFC1929 uses a one-byte length field, so
// the protocol maximum is 255 bytes.
const (
	credUserLen = 6
	credPassLen = 14
	credMaxLen  = 255
)

// newSocksCred generates random SOCKS5 credentials.
func newSocksCred() (SocksCred, error) {
	user, err := randomCredString(credUserLen)
	if err != nil {
		return SocksCred{}, err
	}
	pass, err := randomCredString(credPassLen)
	if err != nil {
		return SocksCred{}, err
	}
	return SocksCred{User: "fo" + user, Pass: pass}, nil
}

func randomCredString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = credAlphabet[int(v)%len(credAlphabet)]
	}
	return string(out), nil
}

// validateCred validates user-provided credentials.
//
// Spaces and URL delimiters are rejected. SOCKS5 itself permits them, but they
// break the socks5://user:pass@host:port format used by most clients.
func validateCred(c SocksCred) error {
	if c.User == "" || c.Pass == "" {
		return fmt.Errorf("username and password cannot be empty")
	}
	if len(c.User) > credMaxLen || len(c.Pass) > credMaxLen {
		return fmt.Errorf("username and password cannot exceed %d bytes", credMaxLen)
	}
	for _, field := range []string{c.User, c.Pass} {
		if strings.ContainsAny(field, ": /@\t\r\n") {
			return fmt.Errorf("username and password cannot contain spaces, colons, slashes, or @")
		}
	}
	return nil
}

// socksServerJSON creates the server entry for an Xray SOCKS outbound.
//
// Both backends use it: local Xray connects to fanout's own SOCKS5 port, so an
// authenticated port must include the same credentials in the outbound config.
func socksServerJSON(t *Tunnel) map[string]any {
	cred := t.credential()
	server := map[string]any{
		"address": "127.0.0.1",
		"port":    t.Port,
	}
	if cred.User != "" {
		server["users"] = []any{map[string]any{
			"user": cred.User,
			"pass": cred.Pass,
		}}
	}
	return server
}

// socksURL builds a socks5:// URL that clients can paste directly.
func socksURL(host string, port int, cred SocksCred) string {
	if cred.User == "" {
		return fmt.Sprintf("socks5://%s:%d", host, port)
	}
	return fmt.Sprintf("socks5://%s:%s@%s:%d", cred.User, cred.Pass, host, port)
}
