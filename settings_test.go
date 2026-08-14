package main

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestNormalizeListenAddr(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"0.0.0.0":   "",
		"all":       "",
		"127.0.0.1": "127.0.0.1",
	}
	for in, want := range cases {
		got, err := normalizeListenAddr(in)
		if err != nil {
			t.Fatalf("normalizeListenAddr(%q) returned an unexpected error: %v", in, err)
		}
		if got != want {
			t.Fatalf("normalizeListenAddr(%q)=%q, want %q", in, got, want)
		}
	}
	if _, err := normalizeListenAddr("not-an-ip"); err == nil {
		t.Fatal("invalid listen address should return an error")
	}
}

func TestValidatePort(t *testing.T) {
	for _, p := range []int{1, 8899, 65535} {
		if err := validatePort(p); err != nil {
			t.Fatalf("port %d should be valid: %v", p, err)
		}
	}
	for _, p := range []int{0, -1, 70000} {
		if err := validatePort(p); err == nil {
			t.Fatalf("port %d should be invalid", p)
		}
	}
}

func TestSetBasePathValidatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	if _, err := initBasePath(dir); err != nil {
		t.Fatalf("initBasePath: %v", err)
	}
	bp, err := setBasePath("myPanel_1")
	if err != nil {
		t.Fatalf("setBasePath: %v", err)
	}
	if bp != "/myPanel_1" || currentBasePath() != "/myPanel_1" {
		t.Fatalf("base path did not take effect: %q / %q", bp, currentBasePath())
	}
	if _, err := os.ReadFile(dir + "/basepath"); err != nil {
		t.Fatalf("base path was not persisted: %v", err)
	}
	if _, err := setBasePath("bad/slash"); err == nil {
		t.Fatal("path containing invalid characters should be rejected")
	}
	// Empty string removes the prefix.
	if bp, err := setBasePath(""); err != nil || bp != "" {
		t.Fatalf("empty path should remove the prefix: %q %v", bp, err)
	}
}

func TestAuthSetPassword(t *testing.T) {
	dir := t.TempDir()
	auth, _, err := NewAuth(dir)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	if err := auth.SetPassword("newsecret"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if !auth.check("newsecret") {
		t.Fatal("new password should validate")
	}
	if auth.check("wrong") {
		t.Fatal("old/wrong password should not validate")
	}
	if err := auth.SetPassword(""); err == nil {
		t.Fatal("empty password should be rejected")
	}
	if err := auth.SetPassword("ab"); err == nil {
		t.Fatal("short password should be rejected")
	}
}

func TestWebServerReloadSwitchesPort(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadWebSettings(dir, 0, false); err != nil {
		t.Fatalf("loadWebSettings: %v", err)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	srv := newWebServer(h)

	// Use two OS-assigned free ports to verify listener switching.
	p1 := freePort(t)
	if err := srv.reload(WebSettings{Port: p1, ListenAddr: "127.0.0.1"}); err != nil {
		t.Fatalf("reload p1: %v", err)
	}
	waitServe(t, p1)

	p2 := freePort(t)
	if err := srv.applyWebSettings(WebSettings{Port: p2, ListenAddr: "127.0.0.1"}); err != nil {
		t.Fatalf("applyWebSettings p2: %v", err)
	}
	waitServe(t, p2)

	// The old port should stop accepting connections after graceful shutdown.
	time.Sleep(1500 * time.Millisecond)
	if c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p1)), 300*time.Millisecond); err == nil {
		c.Close()
		t.Fatalf("old port %d is still listening after the switch", p1)
	}

	// Invalid ports must be rejected without affecting the current listener.
	if err := srv.applyWebSettings(WebSettings{Port: 70000, ListenAddr: "127.0.0.1"}); err == nil {
		t.Fatal("invalid port should be rejected")
	}
	waitServe(t, p2)
}

func waitServe(t *testing.T, port int) {
	t.Helper()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/"
	for i := 0; i < 40; i++ {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("port %d did not begin serving within the expected time", port)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to obtain a free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// Ports changed in the UI are persisted. On a later start with -web explicitly
// supplied, the command-line value must win rather than silently using stale state.
func TestLoadWebSettingsExplicitFlagWins(t *testing.T) {
	dir := t.TempDir()

	// First start: persist 8899.
	if _, err := loadWebSettings(dir, 8899, false); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	// Restart without -web: retain persisted 8899.
	s, err := loadWebSettings(dir, 8899, false)
	if err != nil {
		t.Fatalf("reuse persisted value: %v", err)
	}
	if s.Port != 8899 {
		t.Fatalf("without an explicit flag, persisted value should be retained; got %d", s.Port)
	}

	// Explicit -web 80: command line wins.
	s, err = loadWebSettings(dir, 80, true)
	if err != nil {
		t.Fatalf("explicit value: %v", err)
	}
	if s.Port != 80 {
		t.Fatalf("explicit -web should override persisted value, got %d", s.Port)
	}

	// The explicit value should also be persisted for the next start.
	s, err = loadWebSettings(dir, 8899, false)
	if err != nil {
		t.Fatalf("reload persisted explicit value: %v", err)
	}
	if s.Port != 80 {
		t.Fatalf("explicit port should have been written back, got %d", s.Port)
	}
}
