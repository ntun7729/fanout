package main

import (
	"bufio"
	"io"
	"net"
	"testing"
	"time"
)

// socksClientAuth performs method negotiation and RFC1929 authentication from
// the client side and returns the authentication status byte (0 means success).
func socksClientAuth(t *testing.T, c net.Conn, user, pass string) byte {
	t.Helper()
	// Offer username/password authentication only.
	if _, err := c.Write([]byte{socksVer5, 0x01, authUserPass}); err != nil {
		t.Fatalf("failed to write method negotiation: %v", err)
	}
	r := bufio.NewReader(c)
	sel := make([]byte, 2)
	if _, err := io.ReadFull(r, sel); err != nil {
		t.Fatalf("failed to read method selection: %v", err)
	}
	if sel[0] != socksVer5 || sel[1] != authUserPass {
		t.Fatalf("server did not select username/password authentication: %v", sel)
	}
	msg := []byte{authSubVer, byte(len(user))}
	msg = append(msg, user...)
	msg = append(msg, byte(len(pass)))
	msg = append(msg, pass...)
	if _, err := c.Write(msg); err != nil {
		t.Fatalf("failed to write authentication request: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(r, resp); err != nil {
		t.Fatalf("failed to read authentication result: %v", err)
	}
	return resp[1]
}

func runServeSocks(cred *SocksCred) (net.Conn, func()) {
	sConn, cConn := net.Pipe()
	// dial is never reached because these tests stop after authentication.
	dial := func(network, addr string) (net.Conn, error) { return nil, io.EOF }
	go serveSocks(sConn, cred, dial)
	return cConn, func() { cConn.Close() }
}

func TestSocksAuthAcceptsCorrect(t *testing.T) {
	cred := &SocksCred{User: "alice", Pass: "s3cret"}
	c, done := runServeSocks(cred)
	defer done()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if code := socksClientAuth(t, c, "alice", "s3cret"); code != 0 {
		t.Fatalf("correct credentials should succeed, got status %d", code)
	}
}

func TestSocksAuthRejectsWrong(t *testing.T) {
	cred := &SocksCred{User: "alice", Pass: "s3cret"}
	c, done := runServeSocks(cred)
	defer done()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if code := socksClientAuth(t, c, "alice", "wrong"); code == 0 {
		t.Fatal("incorrect password should be rejected")
	}
}

// A server requiring authentication must reject clients that offer only no-auth.
func TestSocksAuthRejectsNoAuthOffer(t *testing.T) {
	cred := &SocksCred{User: "alice", Pass: "s3cret"}
	c, done := runServeSocks(cred)
	defer done()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := c.Write([]byte{socksVer5, 0x01, authNone}); err != nil {
		t.Fatalf("failed to write method negotiation: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil {
		t.Fatalf("failed to read method selection: %v", err)
	}
	if resp[1] != authNoAccept {
		t.Fatalf("expected 0xFF rejection, got %#x", resp[1])
	}
}

func TestValidateCred(t *testing.T) {
	ok := []SocksCred{
		{User: "fo123", Pass: "abcDEF234"},
		{User: "u", Pass: "p"},
	}
	for _, c := range ok {
		if err := validateCred(c); err != nil {
			t.Errorf("expected valid credentials %+v: %v", c, err)
		}
	}
	bad := []SocksCred{
		{User: "", Pass: "x"},
		{User: "x", Pass: ""},
		{User: "a:b", Pass: "x"},
		{User: "a b", Pass: "x"},
		{User: "x", Pass: "a@b"},
		{User: "x", Pass: "a/b"},
	}
	for _, c := range bad {
		if err := validateCred(c); err == nil {
			t.Errorf("expected invalid credentials %+v to be rejected", c)
		}
	}
}

func TestSocksURL(t *testing.T) {
	got := socksURL("1.2.3.4", 20000, SocksCred{User: "u", Pass: "p"})
	if got != "socks5://u:p@1.2.3.4:20000" {
		t.Fatalf("authenticated URL is incorrect: %s", got)
	}
	got = socksURL("1.2.3.4", 20000, SocksCred{})
	if got != "socks5://1.2.3.4:20000" {
		t.Fatalf("unauthenticated URL is incorrect: %s", got)
	}
}
