package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// checkRealityDest verifies that dest can complete a TLS 1.3 handshake.
//
// REALITY forwards each connection to dest for a real handshake. If that
// handshake cannot complete, the server silently falls back and clients see an
// unhelpful EOF, so validate the destination during node creation instead.
func checkRealityDest(dest, serverName string) error {
	conn, err := net.DialTimeout("tcp", dest, 8*time.Second)
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %w", dest, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	c := tls.Client(conn, &tls.Config{
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	})
	if err := c.Handshake(); err != nil {
		return fmt.Errorf("TLS 1.3 handshake with %s failed: %w", dest, err)
	}
	return nil
}

// realityKeys asks Xray to generate an X25519 key pair.
//
// Output wording differs between versions: newer versions use
// "Password (PublicKey):" while older versions use "Public key:".
func realityKeys(bin string) (priv, pub string, err error) {
	out, err := exec.Command(bin, "x25519").Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate REALITY keys: %w", err)
	}
	text := string(out)

	rePriv := regexp.MustCompile(`(?i)private\s*key:\s*(\S+)`)
	rePub := regexp.MustCompile(`(?i)(?:password\s*\(publickey\)|public\s*key):\s*(\S+)`)

	mp := rePriv.FindStringSubmatch(text)
	mb := rePub.FindStringSubmatch(text)
	if mp == nil || mb == nil {
		return "", "", fmt.Errorf("could not parse xray x25519 output: %s", strings.TrimSpace(text))
	}
	return mp[1], mb[1], nil
}

// randomShortID generates a REALITY shortId. Its hexadecimal length must be
// even and no more than 16 characters.
func randomShortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "0123abcd"
	}
	return hex.EncodeToString(b)
}

// selfSignedCert generates a self-signed certificate for TLS without a real domain.
// OpenSSL is used because Xray needs PEM files on disk and one command handles the job.
func selfSignedCert(dir, serverName string) (certFile, keyFile string, err error) {
	certDir := filepath.Join(dir, "certs")
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return "", "", err
	}
	base := filepath.Join(certDir, sanitizeTag(serverName))
	certFile, keyFile = base+".crt", base+".key"

	cmd := exec.Command("openssl", "req", "-x509", "-nodes",
		"-newkey", "rsa:2048",
		"-days", "3650",
		"-keyout", keyFile,
		"-out", certFile,
		"-subj", "/CN="+serverName,
		"-addext", "subjectAltName=DNS:"+serverName,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("failed to generate self-signed certificate: %s", trimOutput(out))
	}
	return certFile, keyFile, nil
}

// certFingerprint computes the certificate SHA-256 fingerprint in hexadecimal.
// Xray 26.x removed allowInsecure, so self-signed certificates use
// pinnedPeerCertSha256 in share links to pin the generated certificate.
func certFingerprint(certFile string) (string, error) {
	der, err := exec.Command("openssl", "x509", "-in", certFile, "-outform", "der").Output()
	if err != nil {
		return "", fmt.Errorf("failed to read certificate: %w", err)
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

// Supported values shared by frontend/backend validation.
var (
	nativeNetworks   = map[string]bool{"tcp": true, "ws": true, "grpc": true, "httpupgrade": true, "xhttp": true}
	nativeSecurities = map[string]bool{"none": true, "tls": true, "reality": true}
)

// visionCapable reports whether xtls-rprx-vision is valid for the combination.
// Vision works only with VLESS + raw TCP + TLS/REALITY.
func visionCapable(protocol, network, security string) bool {
	return protocol == "vless" && network == "tcp" && (security == "tls" || security == "reality")
}
