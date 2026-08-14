package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// version is injected at build time through -ldflags.
var version = "dev"

func main() {
	var (
		webPort  = flag.Int("web", 8899, "Web management port")
		maxSlots = flag.Int("max", 20, "maximum number of simultaneous tunnels")
		workDir  = flag.String("dir", "/var/lib/fanout", "working directory")
	)
	panelMode := flag.String("panel", "", "node-link backend: blank for saved setting/auto-detect, 3x-ui, native, xray-cf-lite")
	publicIP := flag.String("ip", "", "host public IPv4 used in share links/SOCKS5 addresses; blank for auto-detect")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *publicIP == "" {
		*publicIP = os.Getenv("FANOUT_PUBLIC_IP")
	}

	if *showVersion {
		fmt.Println("fanout", version)
		return
	}

	if os.Geteuid() != 0 {
		log.Fatal("root privileges are required to create network namespaces and change iptables")
	}
	if err := os.MkdirAll(*workDir, 0700); err != nil {
		log.Fatalf("failed to create working directory: %v", err)
	}
	setPublicIPOverride(*publicIP)
	go hostPublicIP() // Warm the probe so the first request does not block.
	if err := prepareHost(); err != nil {
		log.Fatal(err)
	}

	configurePanel(*workDir, *panelMode)
	if p, err := openPanel(); err != nil {
		log.Printf("node-link backend is temporarily unavailable (see the Web UI for details): %v", err)
	} else {
		log.Printf("node-link backend: %s", p.Describe())
	}

	mgr := NewManager(*maxSlots, *workDir)
	log.Printf("fetching node list...")
	if n, err := mgr.RefreshNodes(); err != nil {
		log.Printf("fetch failed (you can retry from the Web UI): %v", err)
	} else {
		log.Printf("fetched %d nodes", n)
	}

	if n, err := mgr.restoreState(); err != nil {
		log.Printf("failed to restore previous state: %v", err)
	} else if n > 0 {
		log.Printf("restoring %d previous tunnels", n)
		// 3x-ui does not automatically rewrite panel outbounds after restart. Older
		// versions also persisted SOCKS outbounds without authentication, so reconcile once.
		go mgr.ReconcileOutbounds()
	}

	go mgr.WatchHealth()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Println("cleaning up all tunnels...")
		mgr.Shutdown()
		closePanel()
		os.Exit(0)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/nodes", apiNodes(mgr))
	mux.HandleFunc("/api/tunnels", apiTunnels(mgr))
	mux.HandleFunc("/api/start", apiStart(mgr))
	mux.HandleFunc("/api/stop", apiStop(mgr))
	mux.HandleFunc("/api/swap", apiSwap(mgr))
	mux.HandleFunc("/api/cred", apiCred(mgr))
	mux.HandleFunc("/api/refresh", apiRefresh(mgr))
	mux.HandleFunc("/api/regions", apiRegions(mgr))
	mux.HandleFunc("/api/provision", apiProvision(mgr))
	mux.HandleFunc("/api/jobs", apiJobs(mgr))
	mux.HandleFunc("/api/jobs/dismiss", apiJobDismiss(mgr))
	mux.HandleFunc("/api/exits", apiExits(mgr))
	mux.HandleFunc("/api/xui", apiXUIStatus)
	mux.HandleFunc("/api/xui/inbounds", apiXUIInbounds(mgr))
	mux.HandleFunc("/api/xui/bind", apiXUIBind(mgr))
	mux.HandleFunc("/api/xui/clone", apiXUIClone(mgr))
	mux.HandleFunc("/api/xui/detail", apiXUIDetail)
	mux.HandleFunc("/api/xui/links", apiXUILinks)
	mux.HandleFunc("/api/xui/delete", apiXUIDelete(mgr))
	mux.HandleFunc("/api/panel/inbound/new", apiInboundCreate(mgr))
	mux.HandleFunc("/api/panel/inbound/update", apiInboundUpdate(mgr))
	mux.HandleFunc("/api/panel/client/add", apiClientAdd(mgr))
	mux.HandleFunc("/api/panel/client/del", apiClientDelete(mgr))
	mux.HandleFunc("/api/panel/client/reset", apiClientReset(mgr))
	mux.HandleFunc("/api/panel/mode", apiPanelMode(*workDir))

	auth, created, err := NewAuth(*workDir)
	if err != nil {
		log.Fatalf("failed to initialize access password: %v", err)
	}
	if created {
		log.Printf("generated access password; see %s", filepath.Join(*workDir, "password"))
	}

	bpCreated, err := initBasePath(*workDir)
	if err != nil {
		log.Fatalf("failed to initialize access path: %v", err)
	}
	if bpCreated {
		log.Printf("generated access path; see %s", filepath.Join(*workDir, "basepath"))
	}

	// An explicit -web value overrides persisted settings; otherwise reuse the saved port.
	portExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "web" {
			portExplicit = true
		}
	})
	webCfg, err := loadWebSettings(*workDir, *webPort, portExplicit)
	if err != nil {
		log.Fatalf("failed to load Web settings: %v", err)
	}

	srv := newWebServer(StripBasePath(auth.Wrap(mux)))
	// Settings panel: password / path / port / local listen address.
	mux.HandleFunc("/api/settings", apiSettings(auth, srv))
	mux.HandleFunc("/api/update/check", apiUpdateCheck)
	mux.HandleFunc("/api/update/apply", apiUpdateApply)

	log.Printf("management UI: http://<host-IP>%s%s/", webCfg.listenAddrString(), currentBasePath())
	log.Printf("SOCKS5 ports are allocated randomly between %d and %d", randPortMin, randPortMax)
	if err := srv.serve(); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func apiNodes(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, fetched := m.Nodes()
		if len(nodes) > 200 {
			nodes = nodes[:200]
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"nodes":   nodes,
			"fetched": fetched,
		})
	}
}

func apiTunnels(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.Tunnels())
	}
}

func apiStart(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.URL.Query().Get("host")
		if host == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing host parameter"})
			return
		}
		nodes, _ := m.Nodes()
		for _, n := range nodes {
			if n.HostName == host {
				t, err := m.Start(n)
				if err != nil {
					writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, t)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found; the list may be stale"})
	}
}

func apiStop(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slot, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid slot parameter"})
			return
		}
		if err := m.Stop(slot); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "stopped"})
	}
}

func apiRefresh(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, err := m.RefreshNodes()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"count": n})
	}
}

// apiSwap switches an exit to another node without changing its port.
func apiSwap(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slot, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid slot parameter"})
			return
		}
		if err := m.Swap(slot); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "switching node"})
	}
}

// apiRegions returns the number of available unused nodes in each region.
func apiRegions(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.Regions())
	}
}

// apiCred changes an exit's SOCKS5 username/password. Leaving both empty generates a new pair.
func apiCred(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		slot, err := strconv.Atoi(q.Get("slot"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid slot parameter"})
			return
		}
		cred, err := m.SetCred(slot, SocksCred{
			User: strings.TrimSpace(q.Get("user")),
			Pass: strings.TrimSpace(q.Get("pass")),
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"user": cred.User,
			"pass": cred.Pass,
		})
	}
}

// apiSettings manages password/path/port/listen address. GET returns current
// values without the plaintext password; POST applies fields that are present.
func apiSettings(auth *Auth, srv *webServer) http.HandlerFunc {
	type settingsReq struct {
		Password   *string `json:"password"`    // Non-empty changes the password.
		BasePath   *string `json:"base_path"`   // Present changes access path; empty removes prefix.
		Port       *int    `json:"port"`        // Present changes listen port.
		ListenAddr *string `json:"listen_addr"` // Present changes listen address.
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var in settingsReq
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request format"})
				return
			}

			if in.Password != nil && *in.Password != "" {
				if err := auth.SetPassword(*in.Password); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
			}
			if in.BasePath != nil {
				if _, err := setBasePath(*in.BasePath); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
			}
			// Apply port and address together so the listener is rebound only once.
			if in.Port != nil || in.ListenAddr != nil {
				next := getWebSettings()
				if in.Port != nil {
					next.Port = *in.Port
				}
				if in.ListenAddr != nil {
					next.ListenAddr = *in.ListenAddr
				}
				if err := srv.applyWebSettings(next); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
			}
		}

		cfg := getWebSettings()
		listen := cfg.ListenAddr
		if listen == "" {
			listen = "0.0.0.0"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"base_path":    currentBasePath(),
			"port":         cfg.Port,
			"listen_addr":  listen,
			"has_password": true,
			"version":      version,
		})
	}
}

// apiUpdateCheck queries the latest GitHub release and returns update status.
func apiUpdateCheck(w http.ResponseWriter, r *http.Request) {
	st, err := checkUpdate()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to check for updates: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// apiUpdateApply downloads the latest version, replaces the binary, and restarts the service.
func apiUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use POST"})
		return
	}
	st, err := checkUpdate()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to check for updates: " + err.Error()})
		return
	}
	if !st.HasUpdate {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restarting": false, "message": "already up to date"})
		return
	}
	if err := applyUpdate(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Return the response before restartSelf fires after its delay.
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restarting": true, "latest": st.Latest})
}

// apiExits returns everything required by the main UI: exits and attached inbounds.
func apiExits(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.ExitsOf())
	}
}

// apiProvision accepts the intent to create N exits in a region and returns a job ID.
func apiProvision(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		count, err := strconv.Atoi(q.Get("count"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid count parameter"})
			return
		}
		tpl := 0
		if s := q.Get("template"); s != "" {
			if tpl, err = strconv.Atoi(s); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid template parameter"})
				return
			}
		}
		job, err := m.Provision(ProvisionRequest{
			Region: q.Get("region"), Count: count, TemplateID: tpl,
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"job": job.ID()})
	}
}

func apiJobs(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.jobs.Views())
	}
}

func apiJobDismiss(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.jobs.Dismiss(r.URL.Query().Get("id"))
		writeJSON(w, http.StatusOK, map[string]string{"ok": "closed"})
	}
}

// apiXUIStatus reports the current node-link backend.
func apiXUIStatus(w http.ResponseWriter, r *http.Request) {
	p, err := openPanel()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    err.Error(),
		})
		return
	}
	// xray-cf-lite owns its nodes; fanout only changes their routing.
	_, isXCL := p.(*XCL)
	resp := map[string]any{
		"available": true,
		"kind":      p.Kind(),
		"describe":  p.Describe(),
		// Native/3x-ui can create inbounds; xray-cf-lite can only change routing.
		"can_create": !isXCL,
	}
	if x, ok := p.(*XUI); ok {
		resp["port"] = x.Port
		resp["base_path"] = x.BasePath
		resp["scheme"] = x.Scheme
		resp["host"] = x.Host
	}
	writeJSON(w, http.StatusOK, resp)
}

// apiPanelMode reads or switches the node-link backend. GET returns current and
// available modes; POST {"mode":"..."} switches at runtime. Empty mode restores auto-detection.
func apiPanelMode(workDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var in struct {
				Mode string `json:"mode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request format"})
				return
			}
			p, err := switchPanelMode(in.Mode)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			invalidateInbounds()
			writeJSON(w, http.StatusOK, map[string]any{
				"mode":     currentPanelMode(),
				"kind":     p.Kind(),
				"describe": p.Describe(),
			})
			return
		}
		resp := map[string]any{
			"mode":  currentPanelMode(),
			"modes": availablePanelModes(workDir),
		}
		if p, err := openPanel(); err == nil {
			resp["kind"] = p.Kind()
			resp["describe"] = p.Describe()
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// apiXUIInbounds lists existing inbounds and their binding state.
func apiXUIInbounds(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := cachedInbounds(liveHosts(m))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// liveHosts returns the identifiers of nodes with connected tunnels.
func liveHosts(m *Manager) map[string]bool {
	live := map[string]bool{}
	for _, t := range m.Tunnels() {
		if t.Status == "up" {
			live[sanitizeTag(t.Node.HostName)] = true
		}
	}
	return live
}

// apiXUIBind binds an inbound to a tunnel. An empty host unbinds it to direct routing.
func apiXUIBind(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag := r.URL.Query().Get("tag")
		if tag == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing tag parameter"})
			return
		}
		host := r.URL.Query().Get("host")
		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if err := x.Bind(tag, host, m.Tunnels()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		invalidateInbounds()
		writeJSON(w, http.StatusOK, map[string]string{"ok": "updated"})
	}
}

// apiXUIClone clones an inbound template for connected tunnels and binds each copy.
func apiXUIClone(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id parameter"})
			return
		}

		tunnels := m.Tunnels()
		// Use node hostnames rather than slot numbers because slots can be reassigned after restart.
		var hosts []string
		if raw := r.URL.Query().Get("hosts"); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				if h := strings.TrimSpace(part); h != "" {
					hosts = append(hosts, h)
				}
			}
		} else {
			for _, t := range tunnels {
				if t.Status == "up" {
					hosts = append(hosts, t.Node.HostName)
				}
			}
		}
		if len(hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no available tunnels"})
			return
		}

		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		ports, err := x.CloneToTunnels(id, hosts, tunnels)
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "created": ports})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"created": ports})
	}
}

// apiXUIDetail returns inbound details, including clients and share links.
func apiXUIDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id parameter"})
		return
	}
	x, err := openPanel()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	host := r.URL.Query().Get("host")
	if host == "" {
		host = publicHost(r)
	}
	detail, err := x.InboundDetail(id, host)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// publicHost chooses the connection address used in share links. Prefer the
// host's public IPv4 because that is what clients actually reach; if detection
// fails, fall back to the hostname used to access fanout.
func publicHost(r *http.Request) string {
	if ip := hostPublicIP(); ip != "" {
		return ip
	}
	host := r.Host
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	if host == "" || host == "127.0.0.1" || host == "localhost" {
		return "<server-IP>"
	}
	return host
}

// apiXUILinks exports share links for multiple inbounds.
func apiXUILinks(w http.ResponseWriter, r *http.Request) {
	x, err := openPanel()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	var ids []int
	if raw := r.URL.Query().Get("ids"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				ids = append(ids, n)
			}
		}
	} else {
		list, err := x.Inbounds(nil)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		for _, ib := range list {
			ids = append(ids, ib.ID)
		}
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no inbounds available to export"})
		return
	}

	host := r.URL.Query().Get("host")
	if host == "" {
		host = publicHost(r)
	}
	links, err := x.InboundLinks(ids, host)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "links": links})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

// apiXUIDelete removes inbounds. Inbounds may remain after an exit is stopped,
// so the UI needs an explicit cleanup path.
func apiXUIDelete(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ids []int
		for _, part := range strings.Split(r.URL.Query().Get("ids"), ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				ids = append(ids, n)
			}
		}
		if len(ids) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no inbounds specified for deletion"})
			return
		}
		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		err = x.DeleteInbounds(ids, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"deleted": len(ids)})
	}
}

// apiInboundUpdate changes an inbound's port, remark, or enabled state for any supported backend.
func apiInboundUpdate(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		q := r.URL.Query()
		id, err := strconv.Atoi(q.Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id parameter"})
			return
		}

		// Change only parameters that were actually supplied.
		var patch InboundPatch
		if v := q.Get("port"); v != "" {
			port, err := strconv.Atoi(v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid port"})
				return
			}
			patch.Port = &port
		}
		if q.Has("remark") {
			remark := q.Get("remark")
			patch.Remark = &remark
		}
		if v := q.Get("enable"); v != "" {
			enable := v == "1"
			patch.Enable = &enable
		}

		err = p.UpdateInbound(id, patch, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "saved"})
	}
}

// clientAction shares ID/email parsing and backend invocation for client operations.
func clientAction(m *Manager, what string,
	do func(p Panel, id int, email string, tunnels []*Tunnel) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id parameter"})
			return
		}
		err = do(p, id, r.URL.Query().Get("email"), m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": what})
	}
}

func apiClientAdd(m *Manager) http.HandlerFunc {
	return clientAction(m, "added", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.AddClient(id, email, t)
	})
}

func apiClientDelete(m *Manager) http.HandlerFunc {
	return clientAction(m, "deleted", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.DeleteClient(id, email, t)
	})
}

func apiClientReset(m *Manager) http.HandlerFunc {
	return clientAction(m, "reset", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.ResetClient(id, email, t)
	})
}

// apiInboundCreate creates a new inbound through any backend that supports creation.
func apiInboundCreate(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		q := r.URL.Query()
		port, _ := strconv.Atoi(q.Get("port"))
		ib, err := p.CreateInbound(NewInboundSpec{
			Protocol: q.Get("protocol"),
			Network:  q.Get("network"),
			Port:     port,
			Remark:   q.Get("remark"),
			Path:     q.Get("path"),
			Host:     q.Get("host"),
			Security: q.Get("security"),
			Vision:   q.Get("vision") == "1",

			ServerName: q.Get("sni"),
			CertFile:   q.Get("cert"),
			KeyFile:    q.Get("key"),

			Dest:        q.Get("dest"),
			ServerNames: q.Get("server_names"),
			ShortID:     q.Get("sid"),
			Fingerprint: q.Get("fp"),
		}, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       ib.ID,
			"port":     ib.Port,
			"protocol": ib.Protocol,
			"remark":   ib.Remark,
			"network":  ib.Network,
			"security": ib.Security,
		})
	}
}
