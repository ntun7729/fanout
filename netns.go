package main

import (
	"net"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// dialerInNetns returns a dial function that establishes outbound connections
// inside the specified network namespace. Each dial switches namespaces because
// a socket's namespace ownership is fixed when the socket is created.
func dialerInNetns(nsName string) func(network, addr string) (net.Conn, error) {
	return func(network, addr string) (net.Conn, error) {
		type result struct {
			conn net.Conn
			err  error
		}
		ch := make(chan result, 1)

		go func() {
			runtime.LockOSThread()

			origin, err := os.Open("/proc/self/ns/net")
			if err != nil {
				ch <- result{nil, err}
				return
			}
			defer origin.Close()

			target, err := os.Open("/var/run/netns/" + nsName)
			if err != nil {
				ch <- result{nil, err}
				return
			}
			defer target.Close()

			if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
				ch <- result{nil, err}
				return
			}

			// Tunnels only have IPv4 routes. Without forcing IPv4, net.Dial could
			// choose an AAAA record and send traffic through the host's IPv6 path,
			// exposing the real host address.
			conn, dialErr := net.Dial(forceIPv4Network(network), addr)

			if err := unix.Setns(int(origin.Fd()), unix.CLONE_NEWNET); err != nil {
				if conn != nil {
					conn.Close()
				}
				ch <- result{nil, err}
				return
			}

			runtime.UnlockOSThread()
			ch <- result{conn, dialErr}
		}()

		r := <-ch
		return r.conn, r.err
	}
}

// forceIPv4Network converts tcp/udp to tcp4/udp4 and leaves explicitly versioned networks unchanged.
func forceIPv4Network(network string) string {
	switch network {
	case "tcp":
		return "tcp4"
	case "udp":
		return "udp4"
	}
	return network
}
