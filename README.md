# fanout

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Turn public VPN Gate nodes into local SOCKS5 ports: one port, one exit IP.
Then attach a proxy node link to each exit, so clients connected to different ports leave through different countries.

There are three ways to manage node links. If 3x-ui or xray-cf-lite is installed on the same machine, fanout takes over their inbounds.
If neither is installed, fanout runs Xray itself. Creating nodes, editing nodes, and exporting links are all handled from the same interface.

![Main dashboard](https://images.joeyblog.net/2026/7/27/fanout-dashboard.png)

Four tunnels can run on one machine, with four ports mapped to four countries. The host machine's own IP and routing remain unaffected:

![Exit verification](https://images.joeyblog.net/2026/7/26/fanout-6-exit-ip.png)

## How it works

Each node runs inside its own network namespace, where the official OpenVPN client is started.
SOCKS5 listens on the host, and outbound connections use `setns` to enter the matching network namespace.

This keeps VPN route changes isolated to each namespace instead of affecting the host network.
Multiple nodes do not interfere with one another, and each gets its own exit IP.

```
Client --> Host SOCKS5 :random-port --> netns foN --> openvpn --> VPN Gate node
```

## Installation

Requires root on Linux because network namespaces are used.

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ntun7729/fanout/main/install.sh)
```

The installer automatically downloads the prebuilt binary for the current architecture. You can also clone the repository and run the same script from the source directory; in that case it builds from source and requires Go 1.21+.

Dependencies such as OpenVPN, curl, OpenSSL, iproute, and iptables are installed automatically for supported distributions.
The installer recognizes apt, dnf, yum, pacman, apk, and zypper. If 3x-ui is not installed, it also downloads Xray into `/var/lib/fanout/bin/`. If 3x-ui is installed, that step is skipped and the panel manages the inbounds.

The service can be installed with either systemd or OpenRC and is enabled to start automatically at boot.

**Alpine** does not include bash by default, so install it first:

```bash
apk add bash curl
bash <(curl -fsSL https://raw.githubusercontent.com/ntun7729/fanout/main/install.sh)
```

fanout runs OpenVPN inside network namespaces, so the **host must expose `/dev/net/tun`**.
Many LXC containers do not have this permission. If `ls /dev/net/tun` shows that it does not exist and `mknod` returns `Operation not permitted`, fanout cannot run on that machine regardless of distribution.

After installation, run `f` to open the management menu:

![Management menu](https://images.joeyblog.net/2026/7/26/fanout-7-menu.png)

The installer prints the management URL, access path, and password:

```
Management UI  http://<your-IP>:8899/gwPuWHvaNr/
Password       f81120ac328d11c11b
```

The path and password are randomly generated and stored in `/var/lib/fanout/basepath` and `/var/lib/fanout/password` respectively.
Requests using the wrong path always receive a 404, which prevents simple port scans from revealing what is running there.

## Usage

The interface is organized around **exits**. Each row represents one tunnel plus the node links attached to it.

Click **New Exit**, choose a region and quantity, then select an existing node as the template. fanout starts the tunnels in parallel, clones a node link for each exit, binds everything together, and reports progress for each target.

![New exit](https://images.joeyblog.net/2026/7/27/fanout-wizard.png)

Each exit row has two actions on the right: replace the VPN node, which changes the exit IP while keeping the port unchanged, or stop the exit.
Because the port stays the same when a node is replaced, already distributed client configurations do not need to be changed.

Click a node name to open its details. You can change its port or note, enable or disable it, manage clients, and bind it to another exit:

![Node details](https://images.joeyblog.net/2026/7/27/fanout-detail.png)

A single inbound can have multiple client credentials for different users. Each credential can be reset independently, and the old link stops working immediately after a reset.

Use **Export Links** to export all node links at once:

![Export links](https://images.joeyblog.net/2026/7/27/fanout-export.png)

### Where node links come from

If 3x-ui is installed on the same machine, fanout takes over its inbounds directly. The panel port, path, and API token are detected automatically, including SSL-enabled installations.
If 3x-ui is not installed, fanout runs its own Xray instance and the interface includes a **New Node** button. You can choose the protocol (VLESS / VMess / Trojan), transport (TCP / WebSocket / gRPC / HTTPUpgrade / XHTTP), and security layer (None / TLS / REALITY).

![New node](https://images.joeyblog.net/2026/7/27/fanout-newnode.png)

REALITY key pairs and short IDs are generated automatically. If TLS is selected without certificate paths, fanout generates a self-signed certificate and includes the certificate fingerprint in shared links so clients can pin it. You can also provide your own certificate paths.

The operations for changing ports, enabling or disabling nodes, adding or deleting clients, and binding exits work the same way whether fanout manages 3x-ui or runs Xray itself.

Machines with [xray-cf-lite](https://github.com/byJoey/xray-cf-lite) installed are detected automatically and fanout takes over the three nodes it creates.
In this mode xray-cf-lite owns the nodes, while fanout only controls which exit each node uses. Therefore the fanout interface does not offer node creation, deletion, or node-setting changes in this mode. Change ports or UUIDs in xray-cf-lite instead.
Both applications share the same Xray configuration. fanout only adds outbound and routing rules with its own prefix and does not overwrite xray-cf-lite's settings.

You can switch backends at any time in Settings. Backends that are not installed locally are disabled and show the reason.
You can also force a backend with `-panel 3x-ui`, `-panel native`, or `-panel xray-cf-lite`.
Selections made in the interface are saved and remain active after restart.

## Operations

After installation, run `f` to open the management menu. It can start or stop the service, show logs, inspect tunnels, change the port, password, or access path, update fanout, and uninstall it.

```
  Status       running
  Version      fanout v0.1.1
  Start at boot enabled

  Management   http://1.2.3.4:8899/gwPuWHvaNr/
  Password     f81120ac328d11c11b

   1) Start          2) Stop
   3) Restart        4) View logs
   5) Tunnel list    6) Connection info
   7) Change port    8) Change password
   9) Change path   10) Startup toggle
  11) Update        12) Uninstall
```

You can also use commands directly:

```bash
f info       # connection information
f list       # tunnel list
f restart    # restart
f log        # follow logs
f update     # update to the latest version
f uninstall  # uninstall
```

Tunnel state is stored in `/var/lib/fanout/state.json`. Tunnels are restored automatically after restart and keep the same ports.

The health check runs every 10 seconds and verifies that each current exit IP still matches the IP recorded when its tunnel was established.
This is more reliable than checking connectivity alone because a dead OpenVPN process can leave its network namespace able to reach the internet through host NAT.
After two consecutive mismatches, fanout automatically selects another node and reconnects. The slot and port remain unchanged, and node links previously pointing to that exit are rebound automatically.

## Known limitations

- TCP forwarding only. If SOCKS5 receives a domain name, it is resolved on the host. UDP/DNS is not tunneled.
- VPN Gate is made up of volunteer nodes, and a significant number may be offline or full (`AUTH_FAILED`). If a node cannot connect at startup, fanout automatically tries other candidates from the same region, up to six nodes.
- The management interface uses a random path and password but does not provide HTTPS itself. If it is exposed publicly, placing it behind a reverse proxy with HTTPS is recommended.

## License

[MIT](LICENSE).

Nodes come from [VPN Gate](https://www.vpngate.net/), an academic experimental project operated by the University of Tsukuba.
This tool only consumes VPN Gate's public node list and connects using the official OpenVPN client. It does not modify or proxy the VPN Gate service.
Use it in accordance with VPN Gate's terms and the laws applicable in your location.

## Community

- Telegram group: <https://t.me/+ft-zI76oovgwNmRh>
- Video tutorials: <https://youtube.com/@joeyblog>
- Blog: <https://joeyblog.net>

For bugs or feature requests, use the community group or open an issue.