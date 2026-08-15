#!/usr/bin/env bash
# fanout installer: install the binary and service (systemd or OpenRC), then enable startup at boot.
#
# Alpine does not include bash by default. Install it first:
#   apk add bash && bash <(curl -fsSL .../install.sh)

set -euo pipefail

# Remember whether WEB_PORT was explicitly supplied. On reinstall, only an
# explicit value should override the saved port.
WEB_PORT_EXPLICIT="${WEB_PORT:+1}"
WEB_PORT="${WEB_PORT:-8899}"
WORK_DIR="${WORK_DIR:-/var/lib/fanout}"
BIN=/usr/local/bin/fanout

if [[ $EUID -ne 0 ]]; then
  echo "root privileges are required to create network namespaces and change iptables" >&2
  exit 1
fi

# Init-system abstraction for systemd and OpenRC.
INIT_SYS=""
if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
  INIT_SYS=systemd
elif command -v rc-service >/dev/null 2>&1; then
  INIT_SYS=openrc
else
  echo "unsupported init system; systemd or OpenRC is required" >&2
  exit 1
fi

# seed_settings stores the port in settings.json, which is authoritative for
# the application, the f menu, and the web interface.
#
# Do not overwrite a port the user already changed during reinstall unless
# WEB_PORT was explicitly supplied this time.
seed_settings() {
  local f="${WORK_DIR}/settings.json"
  if [[ -f "$f" ]] && [[ -z "${WEB_PORT_EXPLICIT:-}" ]]; then
    local cur
    cur=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$f" | head -1)
    [[ -n $cur ]] && { WEB_PORT="$cur"; return; }
  fi
  printf '{\n  "port": %s,\n  "listen_addr": ""\n}\n' "$WEB_PORT" > "$f"
  chmod 600 "$f"
}

svc_install() {
  if [[ "$INIT_SYS" == systemd ]]; then
    # Do not write the port into the service file. It is controlled by
    # ${WORK_DIR}/settings.json. Remove any old -web argument while installing.
    sed "s#-web [0-9]* ##; s#-dir /var/lib/fanout#-dir ${WORK_DIR}#" fanout.service \
      > /etc/systemd/system/fanout.service
    systemctl daemon-reload
  else
    # OpenRC has no systemd unit file, so create an init script directly.
    # supervise-daemon provides process supervision and restart behavior.
    cat > /etc/init.d/fanout <<INITEOF
#!/sbin/openrc-run
name="fanout"
description="fanout - multi-exit gateway"
command="${BIN}"
command_args="-dir ${WORK_DIR}"
command_background=true
pidfile="/run/fanout.pid"
output_log="/var/log/fanout.log"
error_log="/var/log/fanout.log"
respawn_delay=5
respawn_max=0
supervisor=supervise-daemon
depend() { need net; after firewall; }
INITEOF
    chmod +x /etc/init.d/fanout
  fi
}

svc_enable_start() {
  if [[ "$INIT_SYS" == systemd ]]; then
    systemctl enable --now fanout
  else
    rc-update add fanout default >/dev/null 2>&1 || true
    rc-service fanout restart
  fi
}

svc_is_active() {
  if [[ "$INIT_SYS" == systemd ]]; then
    systemctl is-active --quiet fanout
  else
    rc-service fanout status >/dev/null 2>&1
  fi
}

svc_logs_hint() {
  [[ "$INIT_SYS" == systemd ]] && echo "journalctl -u fanout -n 30" || echo "cat /var/log/fanout.log"
}

echo "[1/7] Checking dependencies"

# Package names vary between distributions, so map them by package manager.
pkg_for() {
  local cmd="$1" mgr="$2"
  case "$cmd" in
    openvpn)  echo openvpn ;;
    curl)     echo curl ;;
    openssl)  echo openssl ;;
    tar)      echo tar ;;
    ip)       case "$mgr" in apk|apt-get|pacman) echo iproute2 ;; *) echo iproute ;; esac ;;
    iproute2) echo iproute2 ;;
    iptables) echo iptables ;;
    unzip)    echo unzip ;;
  esac
}

detect_mgr() {
  for m in apt-get dnf yum pacman apk zypper; do
    command -v "$m" >/dev/null && { echo "$m"; return; }
  done
  echo ""
}

install_pkgs() {
  local mgr="$1"; shift
  case "$mgr" in
    apt-get)
      apt-get update -qq
      DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@"
      ;;
    dnf)    dnf install -y -q "$@" ;;
    yum)    yum install -y -q "$@" ;;
    pacman) pacman -Sy --noconfirm --needed "$@" ;;
    apk)    apk add --no-cache "$@" ;;
    zypper) zypper --non-interactive install -y "$@" ;;
  esac
}

MGR=$(detect_mgr)
need_cmd=()
for c in openvpn curl openssl tar iptables; do
  command -v "$c" >/dev/null || need_cmd+=("$c")
done
command -v ip >/dev/null || need_cmd+=(ip)

# Alpine's BusyBox provides an `ip` applet, so command -v ip is not enough.
# fanout requires the full iproute2 implementation for `ip netns`.
if [[ "$MGR" == "apk" ]] && ! apk info -e iproute2 >/dev/null 2>&1; then
  need_cmd+=(iproute2)
fi

if [[ ${#need_cmd[@]} -gt 0 ]]; then
  echo "      Missing: ${need_cmd[*]}"
  if [[ -z "$MGR" ]]; then
    echo "      Unsupported package manager. Install the missing packages manually and retry." >&2
    exit 1
  fi
  pkgs=()
  for c in "${need_cmd[@]}"; do
    pkgs+=("$(pkg_for "$c" "$MGR")")
  done
  echo "      Installing: ${pkgs[*]}"
  install_pkgs "$MGR" "${pkgs[@]}" || {
    echo "      Automatic installation failed. Install manually: ${pkgs[*]}" >&2
    exit 1
  }
fi

echo "[2/7] Installing fanout"
REPO="${REPO:-ntun7729/fanout}"
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) echo "      Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

if [[ -f main.go ]] && command -v go >/dev/null; then
  echo "      Building from source"
  go build -trimpath -ldflags "-s -w" -o "$BIN" .
else
  echo "      Downloading prebuilt release (${GOARCH})"
  TMP=$(mktemp -d)
  URL="https://github.com/${REPO}/releases/latest/download/fanout-linux-${GOARCH}.tar.gz"
  if ! curl -fsSL "$URL" -o "$TMP/f.tar.gz"; then
    echo "      Download failed: $URL" >&2
    echo "      You can also clone the repository and run this script from the source directory." >&2
    exit 1
  fi
  tar xzf "$TMP/f.tar.gz" -C "$TMP"
  install -m 755 "$TMP/fanout" "$BIN"
  [[ -f fanout.service ]] || cp "$TMP/fanout.service" .
  [[ -f "$TMP/f.sh" ]] && install -m 755 "$TMP/f.sh" /usr/local/bin/f
  rm -rf "$TMP"
fi

echo "[3/7] Preparing WARP MASQUE"
# WARP MASQUE is provided by usque. It exposes WARP as a loopback SOCKS5 proxy
# and can carry MASQUE over HTTP/2 + TCP/TLS, so it does not require /dev/net/tun.
mkdir -p "${WORK_DIR}/bin"
USQUE_BIN="${WORK_DIR}/bin/usque"
USQUE_VERSION="4.2.1"
case "$GOARCH" in
  amd64)
    USQUE_ASSET="usque_${USQUE_VERSION}_linux_amd64.zip"
    USQUE_SHA256="4117e20695078af9c11edecd1a826c009bbc7ea0b7f64458612b4198910bc313"
    ;;
  arm64)
    USQUE_ASSET="usque_${USQUE_VERSION}_linux_arm64.zip"
    USQUE_SHA256="c88b061c2a567f30813d7505637d1fb6fe7ec5898b5b61fd05122409fa5ad925"
    ;;
esac

if [[ -x "$USQUE_BIN" ]]; then
  echo "      Existing $("$USQUE_BIN" version 2>/dev/null | head -1)"
else
  UT=$(mktemp -d)
  UURL="https://github.com/Diniboy1123/usque/releases/download/v${USQUE_VERSION}/${USQUE_ASSET}"
  echo "      Downloading usque v${USQUE_VERSION} (${GOARCH})"
  if curl -fsSL "$UURL" -o "$UT/usque.zip"; then
    GOT_SHA=$(openssl dgst -sha256 "$UT/usque.zip" | awk '{print $NF}')
    if [[ "$GOT_SHA" != "$USQUE_SHA256" ]]; then
      echo "      usque checksum mismatch; WARP MASQUE was not installed." >&2
    else
      EXTRACTED=0
      if command -v unzip >/dev/null; then
        unzip -qo "$UT/usque.zip" -d "$UT" && EXTRACTED=1
      elif command -v busybox >/dev/null && busybox unzip -h >/dev/null 2>&1; then
        busybox unzip -qo "$UT/usque.zip" -d "$UT" && EXTRACTED=1
      elif [[ -n "$MGR" ]]; then
        install_pkgs "$MGR" unzip >/dev/null 2>&1 || true
        command -v unzip >/dev/null && unzip -qo "$UT/usque.zip" -d "$UT" && EXTRACTED=1
      fi
      if [[ $EXTRACTED -eq 1 && -f "$UT/usque" ]]; then
        install -m 755 "$UT/usque" "$USQUE_BIN"
        echo "      $("$USQUE_BIN" version 2>/dev/null | head -1)"
      else
        echo "      Could not extract usque; WARP MASQUE will be unavailable until it is installed manually." >&2
      fi
    fi
  else
    echo "      usque download failed; other fanout exit types remain available." >&2
  fi
  rm -rf "$UT"
fi

if [[ -x "$USQUE_BIN" ]]; then
  if [[ -f "${WORK_DIR}/usque/config.json" || -f /var/lib/usque/config.json ]]; then
    echo "      WARP registration config detected"
  else
    echo "      WARP binary installed. Before the first WARP exit, register once:"
    echo "        mkdir -p ${WORK_DIR}/usque && cd ${WORK_DIR}/usque"
    echo "        ${USQUE_BIN} register --accept-tos"
  fi
fi

echo "[4/7] Preparing Xray"
# If no supported panel is available, fanout runs Xray itself. Keep the binary
# under WORK_DIR/bin to avoid conflicting with another Xray installation.
mkdir -p "${WORK_DIR}/bin"
if command -v /usr/local/x-ui/x-ui >/dev/null 2>&1 || [[ -x /usr/bin/x-ui ]]; then
  echo "      Detected 3x-ui; inbounds will be managed by the panel"
elif [[ -d /etc/xray-cf-lite && -f /usr/local/etc/xray/config.json ]]; then
  echo "      Detected xray-cf-lite; inbounds will be managed by it"
elif [[ -x "${WORK_DIR}/bin/xray" ]]; then
  echo "      Existing $("${WORK_DIR}/bin/xray" version 2>/dev/null | head -1)"
else
  case "$GOARCH" in
    amd64) XRAY_ASSET=Xray-linux-64.zip ;;
    arm64) XRAY_ASSET=Xray-linux-arm64-v8a.zip ;;
  esac
  echo "      Downloading Xray (${XRAY_ASSET})"
  XT=$(mktemp -d)
  XURL="https://github.com/XTLS/Xray-core/releases/latest/download/${XRAY_ASSET}"
  if curl -fsSL "$XURL" -o "$XT/x.zip"; then
    # Avoid installing unzip only for one archive when BusyBox can handle it.
    if command -v unzip >/dev/null; then
      unzip -qo "$XT/x.zip" -d "$XT"
    elif command -v busybox >/dev/null && busybox unzip -h >/dev/null 2>&1; then
      busybox unzip -qo "$XT/x.zip" -d "$XT"
    else
      [[ -n "$MGR" ]] && install_pkgs "$MGR" unzip >/dev/null 2>&1 || true
      command -v unzip >/dev/null && unzip -qo "$XT/x.zip" -d "$XT"
    fi
    if [[ -f "$XT/xray" ]]; then
      install -m 755 "$XT/xray" "${WORK_DIR}/bin/xray"
      echo "      $("${WORK_DIR}/bin/xray" version 2>/dev/null | head -1)"
    else
      echo "      Extraction failed. Built-in mode is unavailable, but 3x-ui mode is unaffected." >&2
    fi
  else
    echo "      Download failed. Built-in mode is unavailable, but 3x-ui mode is unaffected." >&2
  fi
  rm -rf "$XT"
fi

echo "[5/7] Enabling forwarding"
sysctl -qw net.ipv4.ip_forward=1
grep -q '^net.ipv4.ip_forward=1' /etc/sysctl.conf 2>/dev/null \
  || echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf
# The FORWARD chain often ends with REJECT, so insert fanout's network rules first.
if ! iptables -C FORWARD -s 10.99.0.0/16 -j ACCEPT 2>/dev/null; then
  iptables -I FORWARD 1 -s 10.99.0.0/16 -j ACCEPT
fi
if ! iptables -C FORWARD -d 10.99.0.0/16 -j ACCEPT 2>/dev/null; then
  iptables -I FORWARD 1 -d 10.99.0.0/16 -j ACCEPT
fi
command -v netfilter-persistent >/dev/null && netfilter-persistent save >/dev/null 2>&1 || true

echo "[6/7] Installing service"
# Management menu.
if [[ -f f.sh ]]; then
  install -m 755 f.sh /usr/local/bin/f
elif [[ -n "${TMP:-}" && -f "${TMP}/f.sh" ]]; then
  install -m 755 "${TMP}/f.sh" /usr/local/bin/f
else
  curl -fsSL "https://raw.githubusercontent.com/${REPO}/main/f.sh" -o /usr/local/bin/f \
    && chmod 755 /usr/local/bin/f
fi
mkdir -p "$WORK_DIR"
chmod 700 "$WORK_DIR"
seed_settings
svc_install
svc_enable_start

echo "[7/7] Ready"
sleep 3
svc_is_active && echo "      Service is running (${INIT_SYS})" || {
  echo "      Service failed to start. Check $(svc_logs_hint)" >&2
  exit 1
}

# fanout generates the password and access path on first start. Wait for them.
for _ in $(seq 1 10); do
  [[ -s "${WORK_DIR}/password" && -s "${WORK_DIR}/basepath" ]] && break
  sleep 1
done

IP=$(curl -s --max-time 8 http://api.ipify.org || echo "<host-IP>")
BP=$(cat "${WORK_DIR}/basepath" 2>/dev/null || true)
# Report the actual configured port so the displayed URL matches the listener.
ACTUAL_PORT=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' \
  "${WORK_DIR}/settings.json" 2>/dev/null | head -1)
[[ -n $ACTUAL_PORT ]] && WEB_PORT="$ACTUAL_PORT"
echo
echo "  Management UI  http://${IP}:${WEB_PORT}/${BP}/"
echo "  Password       $(cat "${WORK_DIR}/password" 2>/dev/null || echo "see ${WORK_DIR}/password")"
echo
echo "  The path and password are generated randomly. You can view them at any time:"
echo "    cat ${WORK_DIR}/basepath"
echo "    cat ${WORK_DIR}/password"
echo
echo "  Run f to open the management menu"
echo
echo "  ────────────────────────────────"
echo "  Telegram  https://t.me/+ft-zI76oovgwNmRh"
echo "  YouTube   https://youtube.com/@joeyblog"
echo "  Blog      https://joeyblog.net"
echo "  Project   https://github.com/ntun7729/fanout"
echo