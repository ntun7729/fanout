# fanout

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Turn multiple public exit sources into local SOCKS5 ports: one port, one exit IP.
fanout supports VPN Gate/OpenVPN exits, independently verified free HTTP/SOCKS proxies, and Cloudflare WARP over MASQUE/TCP.
Then attach a proxy node link to each exit, so clients connected to different ports leave through different egress paths.

There are three ways to manage node links. If 3x-ui or xray-cf-lite is installed on the same machine, fanout takes over their inbounds.
If neither is installed, fanout runs Xray itself. Creating nodes, editing nodes, and exporting links are all handled from the same interface.

![Main dashboard](https://images.joeyblog.net/2026/7/27/fanout-dashboard.png)

Four exits can run on one machine, with four ports mapped to separate egress paths. The host machine's own IP and routing remain unaffected:

![Exit verification](https://images.joeyblog.net/2026/7/26/fanout-6-exit-ip.png)

## How it works

fanout selects an outbound transport per exit while keeping the public SOCKS5 listener on the host:

- **VPN Gate** runs the official OpenVPN client inside a dedicated network namespace. SOCKS5 connections enter that namespace with `setns`, so VPN route changes never replace the host's default route.
- **Free proxies** are validated before being listed and are used as upstream HTTP/SOCKS relays without changing host routes.
- **WARP MASQUE** launches [usque](https://github.com/Diniboy1123/usque) on loopback and chains fanout's SOCKS5 traffic through it. fanout uses usque's HTTP/2 mode, so the WARP/MASQUE connection runs over TCP+TLS on port 443 and does not require `/dev/net/tun`.

```
VPN Gate: Client -> fanout SOCKS5 -> netns foN -> OpenVPN -> VPN Gate
Proxy:    Client -> fanout SOCKS5 -> validated upstream proxy -> Internet
WARP:     Client -> fanout SOCKS5 -> usque SOCKS5 -> MASQUE HTTP/2/TCP 443 -> WARP
```

Each fanout exit keeps its own randomly allocated SOCKS5 port and credentials.

## Installation

fanout itself runs as root because VPN Gate exits use network namespaces and iptables.

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/ntun7729/fanout/main/install.sh)
```

The installer automatically downloads the prebuilt binary for the current architecture. You can also clone the repository and run the same script from the source directory; in that case it builds from source and requires Go 1.23+.

Dependencies such as OpenVPN, curl, OpenSSL, iproute, and iptables are installed automatically for supported distributions.
The installer recognizes apt, dnf, yum, pacman, apk, and zypper. If 3x-ui is not installed, it also downloads Xray into `/var/lib/fanout/bin/`. The installer also downloads the tested usque release into `/var/lib/fanout/bin/usque` for WARP MASQUE support. If 3x-ui is installed, Xray remains panel-managed.

The service can be installed with either systemd or OpenRC and is enabled to start automatically at boot.

**Alpine** does not include bash by default, so install it first:

```bash
apk add bash curl
bash <(curl -fsSL https://raw.githubusercontent.com/ntun7729/fanout/main/install.sh)
```

### LXC and `/dev/net/tun`

VPN Gate/OpenVPN exits require the host to expose `/dev/net/tun`.
Many unprivileged LXC containers do not have this permission. If `ls /dev/net/tun` shows that it does not exist and `mknod` returns `Operation not permitted`, VPN Gate exits cannot run in that container.

**WARP MASQUE and public-proxy exits do not require `/dev/net/tun`.** WARP MASQUE is therefore useful on containers where the provider removed TUN access but normal outbound TCP 443 still works.

### Enable WARP MASQUE

usque needs a one-time Cloudflare WARP registration. fanout does not silently accept Cloudflare's terms on your behalf. After installation, register once if no existing usque configuration is present:

```bash
mkdir -p /var/lib/fanout/usque
cd /var/lib/fanout/usque
/var/lib/fanout/bin/usque register --accept-tos
```

fanout looks for the WARP configuration in this order:

1. `FANOUT_USQUE_CONFIG`
2. `/var/lib/fanout/usque/config.json` (or `<WORK_DIR>/usque/config.json`)
3. `/var/lib/usque/config.json`
4. `/etc/usque/config.json`

The usque binary can be overridden with `FANOUT_USQUE_BIN`. fanout also recognizes `/opt/usque/usque`, `/usr/local/bin/usque`, and `usque` from `PATH`.

Do not publish `config.json`; it contains WARP device credentials.

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

The interface is organized around **exits**. Each row represents one outbound transport plus the node links attached to it.

Click **New Exit**, choose a region and quantity, then select an existing node as the template. fanout starts the exits in parallel, clones a node link for each exit, binds everything together, and reports progress for each target.

![New exit](https://images.joeyblog.net/2026/7/27/fanout-wizard.png)

VPN Gate regions use normal country codes. Validated public proxies use `P-XX` region labels. WARP appears as the `WARP` region and uses **Cloudflare WARP (MASQUE TCP)**. WARP chooses its actual egress location automatically; fanout does not provide country selection for WARP.

Each exit row has actions to replace the selected node when alternatives exist or stop the exit.
Because the public fanout port stays the same during reconnects, already distributed client configurations do not need to be changed.

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

The health check runs every 10 seconds and verifies that each exit can still reach the internet through its selected transport. VPN Gate and public-proxy exits are also checked against their recorded egress IP. WARP may rotate its egress IP while remaining healthy, so fanout accepts a valid WARP response and refreshes the displayed IP. After two consecutive failures, fanout reconnects while keeping the slot and public SOCKS5 port unchanged.

For WARP troubleshooting, each exit writes usque logs to `/var/lib/fanout/foN-usque.log` (or the configured work directory).

## Known limitations

- TCP forwarding only. fanout's public SOCKS5 server accepts CONNECT-style TCP traffic; it does not expose UDP forwarding.
- VPN Gate is made up of volunteer nodes, and a significant number may be offline or full (`AUTH_FAILED`). If a node cannot connect at startup, fanout automatically tries other candidates from the same region, up to six nodes.
- Free public proxies are inherently unstable even though fanout validates them before listing them.
- WARP MASQUE uses an unofficial open-source WARP implementation (usque), requires a Cloudflare WARP registration, and does not provide country selection. Cloudflare selects the egress location.
- The management interface uses a random path and password but does not provide HTTPS itself. If it is exposed publicly, placing it behind a reverse proxy with HTTPS is recommended.

## License

[MIT](LICENSE).

VPN Gate nodes come from [VPN Gate](https://www.vpngate.net/), an academic experimental project operated by the University of Tsukuba.
fanout only consumes VPN Gate's public node list and connects using the official OpenVPN client. It does not modify or proxy the VPN Gate service.

WARP MASQUE support invokes the separate [usque](https://github.com/Diniboy1123/usque) project. usque is not part of fanout and is not an official Cloudflare client. Use WARP and every third-party exit source in accordance with its applicable terms and the laws in your location.

## Community

- Telegram group: <https://t.me/+ft-zI76oovgwNmRh>
- Video tutorials: <https://youtube.com/@joeyblog>
- Blog: <https://joeyblog.net>

For bugs or feature requests, use the community group or open an issue.