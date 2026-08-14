package main

import (
	"testing"
	"time"
)

func newTestAuth() *Auth {
	return &Auth{
		password: "secret",
		sessions: map[string]time.Time{},
		fails:    map[string]*loginFails{},
	}
}

// An IP should be placed in cooldown after reaching the consecutive-failure threshold.
func TestLoginThrottleBlocksAfterMaxFails(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.7"

	for i := 0; i < loginMaxFails; i++ {
		if a.blocked(ip) {
			t.Fatalf("IP should not be blocked before failure %d", i)
		}
		a.recordFail(ip)
	}
	if !a.blocked(ip) {
		t.Fatalf("IP should enter cooldown after %d consecutive failures", loginMaxFails)
	}
}

// A successful login should clear previous failures before the next attempt cycle.
func TestLoginThrottleClearOnSuccess(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.8"

	for i := 0; i < loginMaxFails-1; i++ {
		a.recordFail(ip)
	}
	a.clearFails(ip)
	if a.blocked(ip) {
		t.Fatal("IP should not be blocked after failures are cleared")
	}
	// One new failure after clearing should not immediately trigger cooldown.
	a.recordFail(ip)
	if a.blocked(ip) {
		t.Fatal("a single failure after clearing should not block the IP")
	}
}

// Failure counters must be isolated by source IP.
func TestLoginThrottleIsolatesIPs(t *testing.T) {
	a := newTestAuth()
	for i := 0; i < loginMaxFails; i++ {
		a.recordFail("198.51.100.1")
	}
	if a.blocked("198.51.100.2") {
		t.Fatal("blocking one IP must not affect another IP")
	}
}

// An IP should be unblocked automatically after cooldown expires.
func TestLoginThrottleUnblocksAfterExpiry(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.9"
	for i := 0; i < loginMaxFails; i++ {
		a.recordFail(ip)
	}
	if !a.blocked(ip) {
		t.Fatal("IP should enter cooldown first")
	}
	// Move the block expiry into the past to simulate cooldown completion.
	a.mu.Lock()
	a.fails[ip].blocked = time.Now().Add(-time.Second)
	a.mu.Unlock()
	if a.blocked(ip) {
		t.Fatal("IP should be unblocked after cooldown expires")
	}
}
