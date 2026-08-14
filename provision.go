package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProvisionRequest expresses the intent to create N exits in a region.
type ProvisionRequest struct {
	Region     string // Country code; empty means any region.
	Count      int
	TemplateID int // 3x-ui inbound template; 0 means create tunnels only.
}

// Provision starts a batch exit-provisioning operation asynchronously and
// immediately returns a job handle for UI polling.
//
// Tunnels start in parallel because each must wait for an OpenVPN handshake.
// Panel-side inbound creation is performed serially at the end because every
// routing change requires an Xray restart.
func (m *Manager) Provision(req ProvisionRequest) (*Job, error) {
	if req.Count < 1 {
		return nil, fmt.Errorf("quantity must be at least 1")
	}
	picks, err := m.pickNodes(req.Region, req.Count)
	if err != nil {
		return nil, err
	}

	labels := make([]string, 0, len(picks)+1)
	for _, n := range picks {
		labels = append(labels, regionLabel(n)+" exit")
	}
	if req.TemplateID > 0 {
		labels = append(labels, "Create node links")
	}

	where := req.Region
	if where == "" {
		where = "any region"
	}
	job := m.jobs.New(fmt.Sprintf("Create %d %s exits", len(picks), where), labels)

	go m.runProvision(job, picks, req.TemplateID)
	return job, nil
}

func (m *Manager) runProvision(job *Job, picks []Node, templateID int) {
	defer job.Finish()

	var wg sync.WaitGroup
	started := make([]*Tunnel, len(picks))

	for i, node := range picks {
		t, err := m.Start(node)
		if err != nil {
			job.Set(i, "failed", err.Error())
			continue
		}
		started[i] = t
		job.Set(i, "running", "Connecting "+node.HostName)

		wg.Add(1)
		go func(i int, t *Tunnel) {
			defer wg.Done()
			m.waitUp(t)
			if t.Status == "up" {
				job.Set(i, "ok", t.ExitIP)
				return
			}
			job.Set(i, "failed", firstLine(t.Err))
		}(i, t)
	}
	wg.Wait()

	if templateID <= 0 {
		return
	}

	step := len(picks)
	var hosts []string
	for _, t := range started {
		if t != nil && t.Status == "up" {
			hosts = append(hosts, t.Node.HostName)
		}
	}
	if len(hosts) == 0 {
		job.Set(step, "failed", "No connected exits; skipped")
		return
	}

	job.Set(step, "running", fmt.Sprintf("Creating inbounds for %d exits", len(hosts)))
	x, err := openPanel()
	if err != nil {
		job.Set(step, "failed", err.Error())
		return
	}
	ports, err := x.CloneToTunnels(templateID, hosts, m.Tunnels())
	invalidateInbounds()
	if err != nil {
		job.Set(step, "failed", firstLine(err.Error()))
		return
	}
	job.Set(step, "ok", fmt.Sprintf("Created %d inbounds", len(ports)))
}

// waitUp waits for a tunnel's bringUp process. bringUp tries at most six
// candidate nodes and may wait up to 40 seconds for tun0 on each, so this allows margin.
func (m *Manager) waitUp(t *Tunnel) {
	const maxWait = 5 * time.Minute
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if t.Status == "up" || t.Status == "failed" || t.Status == "stopped" {
			return
		}
		time.Sleep(time.Second)
	}
}

// pickNodes selects up to count unused nodes from the requested region, prioritizing speed.
func (m *Manager) pickNodes(region string, count int) ([]Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	used := map[string]bool{}
	for _, t := range m.tunnels {
		used[t.Node.HostName] = true
	}

	var out []Node
	for _, n := range m.nodes {
		if len(out) >= count {
			break
		}
		if used[n.HostName] {
			continue
		}
		if region != "" && !strings.EqualFold(n.CountryCode, region) {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		if region != "" {
			return nil, fmt.Errorf("no available unused nodes in %s", region)
		}
		return nil, fmt.Errorf("no available unused nodes; try refreshing the list")
	}
	return out, nil
}

// RegionStat summarizes available nodes in one region for the creation wizard.
type RegionStat struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Available int     `json:"available"`
	BestPing  int     `json:"best_ping"`
	BestSpeed float64 `json:"best_speed_mbps"`
}

// Regions summarizes remaining unused nodes by region, sorted by availability.
func (m *Manager) Regions() []RegionStat {
	m.mu.RLock()
	defer m.mu.RUnlock()

	used := map[string]bool{}
	for _, t := range m.tunnels {
		used[t.Node.HostName] = true
	}

	byCode := map[string]*RegionStat{}
	for _, n := range m.nodes {
		if used[n.HostName] || n.CountryCode == "" {
			continue
		}
		s := byCode[n.CountryCode]
		if s == nil {
			s = &RegionStat{Code: n.CountryCode, Name: n.Country, BestPing: n.Ping}
			byCode[n.CountryCode] = s
		}
		s.Available++
		if n.SpeedMbps > s.BestSpeed {
			s.BestSpeed = n.SpeedMbps
		}
		if n.Ping > 0 && (s.BestPing == 0 || n.Ping < s.BestPing) {
			s.BestPing = n.Ping
		}
	}

	out := make([]RegionStat, 0, len(byCode))
	for _, s := range byCode {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Available != out[j].Available {
			return out[i].Available > out[j].Available
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// regionLabel returns a human-readable label for an exit.
func regionLabel(n Node) string {
	if n.CountryCode != "" {
		return n.CountryCode
	}
	return n.HostName
}

// firstLine truncates an error to its first line so it fits in the UI.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}
