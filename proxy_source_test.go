package main

import (
	"reflect"
	"testing"
)

func TestParseProxyList(t *testing.T) {
	body := `
# comment
socks5://72.223.188.92:4145
http://125.122.35.253:8086
https://3.29.67.17:4480
socks4://98.170.57.249:4145
socks5://72.223.188.92:4145
ftp://192.0.2.1:21
not-a-url
http://missing-port.example
`
	got := parseProxyList(body)
	want := []string{
		"socks5://72.223.188.92:4145",
		"http://125.122.35.253:8086",
		"https://3.29.67.17:4480",
		"socks4://98.170.57.249:4145",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseProxyList() = %#v, want %#v", got, want)
	}
}

func TestProxyNodeMarker(t *testing.T) {
	n := Node{Config: proxyNodeMarker + "socks5://127.0.0.1:1080"}
	if !isProxyNode(n) {
		t.Fatal("proxy node marker was not detected")
	}
	if got := proxyURLFromNode(n); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxyURLFromNode() = %q", got)
	}
}

func TestProxyNodeName(t *testing.T) {
	if got := proxyNodeName("socks5://72.223.188.92:4145"); got != "proxy-socks5-72-223-188-92-4145" {
		t.Fatalf("proxyNodeName() = %q", got)
	}
}
