package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// Minimal SOCKS5 implementation supporting CONNECT only.
// Domain names are resolved in this process and the tunnel carries TCP only,
// avoiding any dependency on UDP/DNS inside the tunnel.
//
// Authentication uses RFC1929 username/password. Because the port may be exposed
// publicly, credentials are required to prevent anyone who finds it from using
// the residential exit.

const (
	socksVer5     = 0x05
	authNone      = 0x00
	authUserPass  = 0x02
	authNoAccept  = 0xff
	authSubVer    = 0x01
	cmdConnect    = 0x01
	atypIPv4      = 0x01
	atypDomain    = 0x03
	atypIPv6      = 0x04
	repSuccess    = 0x00
	repGenFail    = 0x01
	repHostUnre   = 0x04
	repCmdNotSupp = 0x07
)

// serveSocks handles one SOCKS5 connection. dial determines which network path
// carries outbound traffic. A nil credential disables auth as a defensive
// fallback, although internal call paths do not use that mode.
func serveSocks(client net.Conn, cred *SocksCred, dial func(network, addr string) (net.Conn, error)) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(30 * time.Second))

	if err := socksHandshake(client, cred); err != nil {
		return
	}

	addr, err := socksReadRequest(client)
	if err != nil {
		if errors.Is(err, errCmdNotSupported) {
			socksReply(client, repCmdNotSupp)
		} else {
			socksReply(client, repGenFail)
		}
		return
	}

	remote, err := dial("tcp", addr)
	if err != nil {
		socksReply(client, repHostUnre)
		return
	}
	defer remote.Close()

	if err := socksReply(client, repSuccess); err != nil {
		return
	}

	// Do not impose an overall relay timeout; let either endpoint close naturally.
	_ = client.SetDeadline(time.Time{})
	_ = remote.SetDeadline(time.Time{})
	relay(client, remote)
}

// socksHandshake negotiates the authentication method and, when required,
// performs RFC1929 username/password authentication.
func socksHandshake(c net.Conn, cred *SocksCred) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil {
		return err
	}
	if head[0] != socksVer5 {
		return errors.New("not SOCKS5")
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}

	if cred == nil || cred.User == "" {
		_, err := c.Write([]byte{socksVer5, authNone})
		return err
	}

	// Reject clients that do not offer username/password auth; never fall back to no auth.
	if !bytes.ContainsRune(methods, rune(authUserPass)) {
		_, _ = c.Write([]byte{socksVer5, authNoAccept})
		return errors.New("client does not support username/password authentication")
	}
	if _, err := c.Write([]byte{socksVer5, authUserPass}); err != nil {
		return err
	}
	return socksAuth(c, cred)
}

// socksAuth performs RFC1929 username/password sub-negotiation.
func socksAuth(c net.Conn, cred *SocksCred) error {
	ver := make([]byte, 1)
	if _, err := io.ReadFull(c, ver); err != nil {
		return err
	}
	if ver[0] != authSubVer {
		return errors.New("invalid authentication sub-protocol version")
	}
	user, err := readLenPrefixed(c)
	if err != nil {
		return err
	}
	pass, err := readLenPrefixed(c)
	if err != nil {
		return err
	}

	// Constant-time comparison avoids leaking credential length/prefix information byte by byte.
	okUser := subtle.ConstantTimeCompare(user, []byte(cred.User)) == 1
	okPass := subtle.ConstantTimeCompare(pass, []byte(cred.Pass)) == 1
	if !okUser || !okPass {
		_, _ = c.Write([]byte{authSubVer, 0x01})
		return errors.New("incorrect username or password")
	}
	_, err = c.Write([]byte{authSubVer, 0x00})
	return err
}

// readLenPrefixed reads a field prefixed by a one-byte length.
func readLenPrefixed(c net.Conn) ([]byte, error) {
	l := make([]byte, 1)
	if _, err := io.ReadFull(c, l); err != nil {
		return nil, err
	}
	b := make([]byte, int(l[0]))
	if _, err := io.ReadFull(c, b); err != nil {
		return nil, err
	}
	return b, nil
}

var errCmdNotSupported = errors.New("only CONNECT is supported")

// errIPv6NotSupported rejects IPv6 destinations because tunnels only provide IPv4 routing.
var errIPv6NotSupported = errors.New("IPv6 is not supported inside tunnels")

func socksReadRequest(c net.Conn) (string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		return "", err
	}
	if head[1] != cmdConnect {
		return "", errCmdNotSupported
	}

	var host string
	switch head[3] {
	case atypIPv4:
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case atypIPv6:
		// There is no IPv6 route inside the tunnel; allowing this would bypass it.
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		return "", errIPv6NotSupported
	case atypDomain:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return "", err
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", fmt.Errorf("unsupported address type %d", head[3])
	}

	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb)))), nil
}

func socksReply(c net.Conn, code byte) error {
	_, err := c.Write([]byte{socksVer5, code, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
	return err
}

func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
