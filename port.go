package main

import (
	"fmt"
	"math/rand"
	"net"
)

// Random port range within the IANA dynamic-port range, avoiding common services.
const (
	randPortMin = 20000
	randPortMax = 60000
)

// freeRandomPort chooses a currently available TCP port at random.
//
// Ports in taken are skipped so allocations made by this process but not yet
// listening are not reused. Actual availability is verified by binding the port,
// which also avoids collisions with other system processes.
func freeRandomPort(taken map[int]bool) (int, error) {
	for i := 0; i < 200; i++ {
		port := randPortMin + rand.Intn(randPortMax-randPortMin)
		if taken[port] {
			continue
		}
		if portAvailable(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port found after 200 attempts")
}

// portAvailable checks whether a port is actually free by trying to listen on it.
func portAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
