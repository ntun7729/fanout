package main

import (
	"reflect"
	"testing"
)

func TestProxyProbeURL(t *testing.T) {
	cases := map[string]string{
		"socks5://127.0.0.1:1080": "https://api.ipify.org",
		"socks5://4.4.4.4:1080":   "http://api.iplocate.io/ip",
		"socks4://127.0.0.1:1080": "http://api.iplocate.io/ip",
		"http://127.0.0.1:8080":   "https://api.iplocate.io/ip",
		"https://127.0.0.1:8443":  "https://api.iplocate.io/ip",
	}
	for raw, want := range cases {
		if got := proxyProbeURL(raw); got != want {
			t.Fatalf("proxyProbeURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestPrioritizeProxyCandidates(t *testing.T) {
	in := []string{
		"http://1.1.1.1:80",
		"https://2.2.2.2:443",
		"socks4://3.3.3.3:1080",
		"socks5://4.4.4.4:1080",
		"socks5://5.5.5.5:1080",
	}
	want := []string{
		"socks5://4.4.4.4:1080",
		"socks5://5.5.5.5:1080",
		"socks4://3.3.3.3:1080",
		"https://2.2.2.2:443",
		"http://1.1.1.1:80",
	}
	if got := prioritizeProxyCandidates(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("priority order = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(in, []string{
		"http://1.1.1.1:80",
		"https://2.2.2.2:443",
		"socks4://3.3.3.3:1080",
		"socks5://4.4.4.4:1080",
		"socks5://5.5.5.5:1080",
	}) {
		t.Fatal("prioritizeProxyCandidates mutated its input")
	}
}
