package main

import "testing"

func mgrWith(nodes []Node, running ...string) *Manager {
	m := NewManager(20, t_tmpdir)
	m.nodes = nodes
	for i, h := range running {
		m.tunnels[i+1] = &Tunnel{Slot: i + 1, Node: Node{HostName: h}, Status: "up"}
	}
	return m
}

const t_tmpdir = "/tmp"

var sample = []Node{
	{HostName: "jp1", CountryCode: "JP", Country: "Japan", SpeedMbps: 300, Ping: 10},
	{HostName: "jp2", CountryCode: "JP", Country: "Japan", SpeedMbps: 200, Ping: 20},
	{HostName: "kr1", CountryCode: "KR", Country: "Korea", SpeedMbps: 150, Ping: 30},
}

func TestPickNodesSkipsRunning(t *testing.T) {
	m := mgrWith(sample, "jp1")
	got, err := m.pickNodes("JP", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].HostName != "jp2" {
		t.Fatalf("expected unused jp2, got %+v", got)
	}
}

func TestPickNodesRegionMismatch(t *testing.T) {
	m := mgrWith(sample, "kr1")
	if _, err := m.pickNodes("KR", 1); err == nil {
		t.Fatal("KR has only one node and it is already in use; expected an error")
	}
}

func TestRegionsExcludesRunning(t *testing.T) {
	m := mgrWith(sample, "jp1")
	for _, r := range m.Regions() {
		if r.Code == "JP" && r.Available != 1 {
			t.Fatalf("JP should have 1 available node, got %d", r.Available)
		}
		if r.Code == "KR" && r.BestSpeed != 150 {
			t.Fatalf("KR best speed should be 150, got %v", r.BestSpeed)
		}
	}
}

func TestJobLifecycle(t *testing.T) {
	var s JobStore
	j := s.New("test", []string{"a", "b"})
	if v := j.View(); v.Total != 2 || v.Done != 0 || v.Status != "running" {
		t.Fatalf("unexpected initial state: %+v", v)
	}

	j.Set(0, "ok", "1.2.3.4")
	j.Set(1, "failed", "connection failed")
	j.Finish()

	v := j.View()
	if v.Status != "failed" || v.Done != 2 {
		t.Fatalf("job should be failed when any step fails: %+v", v)
	}
	if v.Steps[0].Detail != "1.2.3.4" {
		t.Fatalf("step detail was lost: %+v", v.Steps[0])
	}

	s.Dismiss(j.ID())
	if len(s.Views()) != 0 {
		t.Fatal("job should be removed after Dismiss")
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("first line\nsecond line"); got != "first line" {
		t.Fatalf("firstLine = %q", got)
	}
	if got := firstLine("single line"); got != "single line" {
		t.Fatalf("firstLine = %q", got)
	}
}
