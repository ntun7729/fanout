package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// nativeClient is one set of client credentials. Cloned inbounds reuse the same
// client credentials across exits so switching exits only requires changing the port.
type nativeClient struct {
	Email    string `json:"email"`
	ID       string `json:"id"`       // UUID for VLESS/VMess.
	Password string `json:"password"` // Password for Trojan.
	Enable   bool   `json:"enable"`
	// Flow only applies to VLESS and is either empty or xtls-rprx-vision.
	// Vision requires TCP + TLS/REALITY; Xray rejects other combinations.
	Flow string `json:"flow,omitempty"`
}

// nativeInbound represents one inbound managed by fanout's native backend.
// Fields intentionally mirror 3x-ui semantics so both backends behave the same in the UI.
type nativeInbound struct {
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // vless | vmess | trojan
	Network  string `json:"network"`  // tcp | ws | grpc | httpupgrade | xhttp
	Path     string `json:"path"`     // Path for ws/httpupgrade/xhttp; serviceName for gRPC.
	Host     string `json:"host"`     // Host header for ws/httpupgrade/xhttp.
	// Security is transport security: none | tls | reality.
	Security string         `json:"security"`
	TLS      *tlsConfig     `json:"tls,omitempty"`
	Reality  *realityConfig `json:"reality,omitempty"`
	Remark   string         `json:"remark"`
	Enable   bool           `json:"enable"`
	Clients  []nativeClient `json:"clients"`
	// BoundTo is the sanitized hostname of the bound node; empty means direct routing.
	BoundTo string `json:"bound_to"`
}

// tlsConfig stores standard TLS settings. Certificates are either user-provided
// or self-signed by fanout.
type tlsConfig struct {
	ServerName string `json:"server_name"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	// SelfSigned records whether fanout generated the certificate.
	SelfSigned bool `json:"self_signed"`
	// CertSha256 is the certificate's SHA-256 fingerprint in hexadecimal. Modern
	// Xray clients can pin this fingerprint instead of relying on allowInsecure.
	CertSha256 string `json:"cert_sha256,omitempty"`
}

// realityConfig stores REALITY settings. PublicKey is not needed by the server
// but is persisted because clients require it in share links.
type realityConfig struct {
	Dest        string   `json:"dest"` // Real destination site, e.g. www.microsoft.com:443.
	ServerNames []string `json:"server_names"`
	PrivateKey  string   `json:"private_key"`
	PublicKey   string   `json:"public_key"`
	ShortIDs    []string `json:"short_ids"`
	Fingerprint string   `json:"fingerprint"` // Client fingerprint, e.g. chrome.
}

// tag reconstructs the Xray inboundTag using the same format as 3x-ui.
func (n *nativeInbound) tag() string {
	return fmt.Sprintf("in-%d-%s", n.Port, n.netOrTCP())
}

func (n *nativeInbound) netOrTCP() string {
	if n.Network == "" {
		return "tcp"
	}
	return n.Network
}

func (n *nativeInbound) securityOrNone() string {
	if n.Security == "" {
		return "none"
	}
	return n.Security
}

// nativeStore is the persisted state for native mode.
type nativeStore struct {
	NextID   int              `json:"next_id"`
	Inbounds []*nativeInbound `json:"inbounds"`
}

func nativeStatePath(dir string) string { return filepath.Join(dir, "native.json") }

func loadNativeStore(dir string) (*nativeStore, error) {
	blob, err := os.ReadFile(nativeStatePath(dir))
	if os.IsNotExist(err) {
		return &nativeStore{NextID: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var st nativeStore
	if err := json.Unmarshal(blob, &st); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", nativeStatePath(dir), err)
	}
	if st.NextID < 1 {
		st.NextID = 1
	}
	return &st, nil
}

func (s *nativeStore) save(dir string) error {
	blob, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := nativeStatePath(dir) + ".tmp"
	if err := os.WriteFile(tmp, blob, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, nativeStatePath(dir))
}

func (s *nativeStore) byID(id int) *nativeInbound {
	for _, ib := range s.Inbounds {
		if ib.ID == id {
			return ib
		}
	}
	return nil
}

func (s *nativeStore) usedPorts() map[int]bool {
	// Include ports used by external tools such as xray-cf-lite before adding our
	// own inbounds, so both random allocation and manual validation avoid collisions.
	used := externalUsedPorts()
	for _, ib := range s.Inbounds {
		used[ib.Port] = true
	}
	return used
}

// sorted returns inbounds by port for stable UI ordering.
func (s *nativeStore) sorted() []*nativeInbound {
	out := make([]*nativeInbound, len(s.Inbounds))
	copy(out, s.Inbounds)
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// newUUID generates an Xray-compatible UUID v4.
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a still-unique form if the random source is unavailable.
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", os.Getpid())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return strings.Join([]string{h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]}, "-")
}

// randomHex generates n random bytes encoded as hexadecimal for Trojan passwords and transport paths.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprint(os.Getpid())))
	}
	return hex.EncodeToString(b)
}
