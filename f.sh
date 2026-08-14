#!/usr/bin/env bash
# fanout management menu
set -uo pipefail

WORK_DIR=/var/lib/fanout
SERVICE=fanout
BIN=/usr/local/bin/fanout
REPO="${REPO:-ntun7729/fanout}"

G='\033[0;32m'; R='\033[0;31m'; Y='\033[0;33m'; B='\033[0;36m'; D='\033[2m'; N='\033[0m'

need_root() {
  [[ $EUID -eq 0 ]] || { echo -e "${R}root privileges are required${N}"; exit 1; }
}

# Init-system abstraction: systemd and OpenRC.
if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
  INIT_SYS=systemd
  UNIT=/etc/systemd/system/${SERVICE}.service
else
  INIT_SYS=openrc
  UNIT=/etc/init.d/${SERVICE}
fi

svc_start()   { [[ $INIT_SYS == systemd ]] && systemctl start "$SERVICE"   || rc-service "$SERVICE" start; }
svc_stop()    { [[ $INIT_SYS == systemd ]] && systemctl stop "$SERVICE"    || rc-service "$SERVICE" stop; }
svc_restart() { [[ $INIT_SYS == systemd ]] && systemctl restart "$SERVICE" || rc-service "$SERVICE" restart; }
svc_reload()  { [[ $INIT_SYS == systemd ]] && systemctl daemon-reload || true; }
svc_enable()  { [[ $INIT_SYS == systemd ]] && systemctl enable "$SERVICE" >/dev/null 2>&1 || rc-update add "$SERVICE" default >/dev/null 2>&1; }
svc_disable() { [[ $INIT_SYS == systemd ]] && systemctl disable "$SERVICE" >/dev/null 2>&1 || rc-update del "$SERVICE" default >/dev/null 2>&1; }

svc_is_enabled() {
  if [[ $INIT_SYS == systemd ]]; then
    systemctl is-enabled --quiet "$SERVICE"
  else
    rc-update show default 2>/dev/null | grep -q "^ *${SERVICE} "
  fi
}

svc_enabled_text() {
  svc_is_enabled && echo enabled || echo disabled
}

svc_status_page() {
  if [[ $INIT_SYS == systemd ]]; then
    systemctl status "$SERVICE" --no-pager
  else
    rc-service "$SERVICE" status
  fi
}

svc_logs() {
  if [[ $INIT_SYS == systemd ]]; then
    journalctl -u "$SERVICE" -n "${1:-50}" --no-pager
  else
    tail -n "${1:-50}" /var/log/${SERVICE}.log 2>/dev/null || echo "  No logs yet"
  fi
}

svc_logs_follow() {
  if [[ $INIT_SYS == systemd ]]; then
    journalctl -u "$SERVICE" -f
  else
    tail -f /var/log/${SERVICE}.log
  fi
}

svc_state() {
  if [[ $INIT_SYS == systemd ]]; then
    systemctl is-active --quiet "$SERVICE" && echo running || echo stopped
  else
    rc-service "$SERVICE" status >/dev/null 2>&1 && echo running || echo stopped
  fi
}

# settings.json is the authoritative source for the web port. Older versions
# hard-coded -web in the service file, which could restore a stale value.
web_port() {
  local p
  p=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' \
        "$WORK_DIR/settings.json" 2>/dev/null | head -1)
  [[ -n $p ]] && { echo "$p"; return; }
  # Backward compatibility: read the service file before settings.json exists.
  grep -oE '\-web [0-9]+' "$UNIT" 2>/dev/null \
    | grep -oE '[0-9]+' | head -1 || echo 8899
}

public_ip() {
  curl -s --max-time 6 http://api.ipify.org 2>/dev/null || echo "<host-IP>"
}

pause() {
  echo
  read -rp "Press Enter to return to the menu..." _
}

show_info() {
  local state port bp pw ip
  state=$(svc_state); port=$(web_port)
  bp=$(cat "$WORK_DIR/basepath" 2>/dev/null || echo "-")
  pw=$(cat "$WORK_DIR/password" 2>/dev/null || echo "-")
  ip=$(public_ip)

  echo
  if [[ $state == running ]]; then
    echo -e "  Status        ${G}running${N}"
  else
    echo -e "  Status        ${R}stopped${N}"
  fi
  echo -e "  Version       $("$BIN" -version 2>/dev/null || echo '-')"
  echo -e "  Start at boot $(svc_enabled_text)"
  echo
  echo -e "  ${B}Management URL  http://${ip}:${port}/${bp}/${N}"
  echo -e "  ${B}Password        ${pw}${N}"
  echo

  local n
  n=$(ls -d /var/run/netns/fo* 2>/dev/null | wc -l | tr -d ' ')
  echo -e "  ${D}Running tunnels: ${n}${N}"
}

list_tunnels() {
  local port bp pw ck
  port=$(web_port)
  bp=$(cat "$WORK_DIR/basepath" 2>/dev/null)
  pw=$(cat "$WORK_DIR/password" 2>/dev/null)
  ck=$(mktemp)

  curl -s --max-time 10 -c "$ck" -X POST -d "password=${pw}" \
    "http://127.0.0.1:${port}/${bp}/login" -o /dev/null
  echo
  curl -s --max-time 10 -b "$ck" "http://127.0.0.1:${port}/${bp}/api/tunnels" \
    > "$ck.json" 2>/dev/null
  rm -f "$ck"

  # Parse with sed/awk instead of python3/jq because minimal Alpine systems may
  # not include either dependency. The JSON fields are fixed and predictable.
  if [[ ! -s "$ck.json" ]] || ! grep -q '"port"' "$ck.json" 2>/dev/null; then
    echo "  No tunnels yet. Add one from the web interface."
  else
    printf "  %-10s%-11s%-18s%s\n" "Port" "Status" "Exit IP" "Node"
    # Split on {"slot" rather than }, because node is a nested object.
    sed 's/{"slot"/\n{"slot"/g' "$ck.json" | while IFS= read -r line; do
      case "$line" in *'"slot"'*) ;; *) continue ;; esac
      p=$(echo "$line"  | sed -n 's/.*"port":\([0-9]*\).*/\1/p')
      st=$(echo "$line" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
      ip=$(echo "$line" | sed -n 's/.*"exit_ip":"\([^"]*\)".*/\1/p')
      hn=$(echo "$line" | sed -n 's/.*"hostname":"\([^"]*\)".*/\1/p')
      [[ -z $p ]] && continue
      printf "  %-10s%-11s%-18s%s\n" "$p" "${st:--}" "${ip:--}" "${hn:--}"
    done
  fi
  rm -f "$ck.json"
}

change_port() {
  local cur new
  cur=$(web_port)
  echo
  read -rp "  New port (current ${cur}): " new
  [[ -z $new ]] && { echo "  No changes made"; return; }
  if ! [[ $new =~ ^[0-9]+$ ]] || (( new < 1 || new > 65535 )); then
    echo -e "  ${R}Invalid port${N}"; return
  fi
  if ss -tln 2>/dev/null | grep -q ":${new} "; then
    echo -e "  ${R}Port ${new} is already in use${N}"; return
  fi
  # Write settings.json (the authoritative source), and also synchronize any
  # old -web argument left in the service file for backward compatibility.
  if [[ -f "$WORK_DIR/settings.json" ]]; then
    sed -i "s/\"port\"[[:space:]]*:[[:space:]]*[0-9]*/\"port\": ${new}/" "$WORK_DIR/settings.json"
  else
    printf '{\n  "port": %s,\n  "listen_addr": ""\n}\n' "$new" > "$WORK_DIR/settings.json"
    chmod 600 "$WORK_DIR/settings.json"
  fi
  sed -i "s/-web ${cur}/-web ${new}/" "$UNIT" 2>/dev/null
  svc_reload
  svc_restart
  echo -e "  ${G}Changed to ${new} and restarted${N}"
}

reset_password() {
  local pw
  echo
  read -rp "  New password (leave blank to generate one): " pw
  if [[ -z $pw ]]; then
    pw=$(head -c 9 /dev/urandom | od -An -tx1 | tr -d ' \n')
  fi
  umask 077
  echo "$pw" > "$WORK_DIR/password"
  svc_restart
  echo -e "  ${G}New password: ${pw}${N}"
}

reset_basepath() {
  local bp
  echo
  read -rp "  New access path (leave blank to generate one): " bp
  if [[ -z $bp ]]; then
    rm -f "$WORK_DIR/basepath"
    svc_restart
    sleep 2
    bp=$(cat "$WORK_DIR/basepath" 2>/dev/null)
  else
    bp=${bp#/}; bp=${bp%/}
    umask 077
    echo "$bp" > "$WORK_DIR/basepath"
    svc_restart
  fi
  echo -e "  ${G}New path: /${bp}/${N}"
}

ipv6_state() {
  local a d
  a=$(sysctl -n net.ipv6.conf.all.disable_ipv6 2>/dev/null || echo 0)
  d=$(sysctl -n net.ipv6.conf.default.disable_ipv6 2>/dev/null || echo 0)
  [[ "$a" == 1 && "$d" == 1 ]] && echo disabled || echo enabled
}

toggle_ipv6() {
  local conf=/etc/sysctl.d/99-fanout-ipv6.conf
  echo
  if [[ $(ipv6_state) == disabled ]]; then
    read -rp "  IPv6 is currently disabled. Re-enable it? [y/N]: " yes
    [[ ${yes,,} == y ]] || { echo "  Cancelled"; return; }
    rm -f "$conf"
    sysctl -qw net.ipv6.conf.all.disable_ipv6=0
    sysctl -qw net.ipv6.conf.default.disable_ipv6=0
    sysctl -qw net.ipv6.conf.lo.disable_ipv6=0
    echo -e "  ${G}IPv6 has been re-enabled${N}"
    return
  fi

  echo -e "  ${D}If the host has global IPv6, traffic outside the tunnel may leave through IPv6 and expose the real address.${N}"
  read -rp "  Disable IPv6 on the entire host? [y/N]: " yes
  [[ ${yes,,} == y ]] || { echo "  Cancelled"; return; }

  cat > "$conf" <<EOF
net.ipv6.conf.all.disable_ipv6 = 1
net.ipv6.conf.default.disable_ipv6 = 1
net.ipv6.conf.lo.disable_ipv6 = 1
EOF
  sysctl -qw net.ipv6.conf.all.disable_ipv6=1
  sysctl -qw net.ipv6.conf.default.disable_ipv6=1
  sysctl -qw net.ipv6.conf.lo.disable_ipv6=1
  svc_restart >/dev/null 2>&1
  echo -e "  ${G}IPv6 disabled and the setting will persist after reboot${N}"
}

show_links() {
  echo
  echo -e "  Telegram  ${B}https://t.me/+ft-zI76oovgwNmRh${N}"
  echo -e "  YouTube   ${B}https://youtube.com/@joeyblog${N}"
  echo -e "  Blog      ${B}https://joeyblog.net${N}"
  echo -e "  Project   ${B}https://github.com/ntun7729/fanout${N}"
  echo
  echo -e "  ${D}For bugs or feature requests, use the community group or open an issue.${N}"
}

# Older versions hard-coded -web in the service file. During updates, migrate
# that value into settings.json and remove the duplicate service argument.
migrate_port_to_settings() {
  local unit_port
  unit_port=$(grep -oE '\-web [0-9]+' "$UNIT" 2>/dev/null | grep -oE '[0-9]+' | head -1)
  [[ -z $unit_port ]] && return

  if [[ ! -f "$WORK_DIR/settings.json" ]]; then
    printf '{\n  "port": %s,\n  "listen_addr": ""\n}\n' "$unit_port" > "$WORK_DIR/settings.json"
    chmod 600 "$WORK_DIR/settings.json"
  fi
  sed -i "s/-web ${unit_port} //" "$UNIT"
  svc_reload
  echo "  Migrated port ${unit_port} to settings.json"
}

do_update() {
  local arch goarch tmp
  arch=$(uname -m)
  case "$arch" in
    x86_64) goarch=amd64 ;;
    aarch64|arm64) goarch=arm64 ;;
    *) echo -e "  ${R}Unsupported architecture: ${arch}${N}"; return ;;
  esac

  echo -e "\n  Current $("$BIN" -version 2>/dev/null || echo '-')"
  tmp=$(mktemp -d)
  echo "  Downloading the latest version..."
  if ! curl -fsSL "https://github.com/${REPO}/releases/latest/download/fanout-linux-${goarch}.tar.gz" \
       -o "$tmp/f.tar.gz"; then
    echo -e "  ${R}Download failed${N}"; rm -rf "$tmp"; return
  fi
  tar xzf "$tmp/f.tar.gz" -C "$tmp"
  svc_stop
  install -m 755 "$tmp/fanout" "$BIN"
  migrate_port_to_settings
  svc_start
  rm -rf "$tmp"
  echo -e "  ${G}Updated to $("$BIN" -version 2>/dev/null)${N}"
}

do_uninstall() {
  local yes
  echo
  read -rp "  Uninstall fanout? Tunnels and configuration will be deleted. [y/N]: " yes
  [[ ${yes,,} == y ]] || { echo "  Cancelled"; return; }

  svc_stop >/dev/null 2>&1
  svc_disable
  # Remove leftover network namespaces and veth devices.
  for ns in $(ip netns list 2>/dev/null | awk '{print $1}' | grep '^fo[0-9]'); do
    ip netns del "$ns" 2>/dev/null
  done
  for l in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | grep '^fov[0-9]'); do
    ip link del "$l" 2>/dev/null
  done
  rm -f "$UNIT" "$BIN" /usr/local/bin/f
  rm -rf "$WORK_DIR"
  svc_reload
  echo -e "  ${G}Uninstalled${N}"
  exit 0
}

menu() {
  while true; do
    clear
    echo -e "${B}  fanout${N}  ${D}VPN Gate exit fan-out gateway${N}"
    show_info
    echo -e "${D}  ─────────────────────────────${N}"
    echo "   1) Start          2) Stop"
    echo "   3) Restart        4) View logs"
    echo
    echo "   5) Tunnel list    6) Connection info"
    echo
    echo "   7) Change port    8) Change password"
    echo "   9) Change path   10) Startup toggle"
    echo
    echo "  11) Update        12) Uninstall"
    echo "  13) Community / Feedback"
    echo "   0) Exit"
    echo -e "${D}  ─────────────────────────────${N}"
    read -rp "  Select: " choice

    case "$choice" in
      1) svc_start   && echo -e "\n  ${G}Started${N}"; pause ;;
      2) svc_stop    && echo -e "\n  ${Y}Stopped${N}"; pause ;;
      3) svc_restart && echo -e "\n  ${G}Restarted${N}"; pause ;;
      4) echo; svc_logs 40; pause ;;
      5) list_tunnels; pause ;;
      6) show_info; pause ;;
      7) change_port; pause ;;
      8) reset_password; pause ;;
      9) reset_basepath; pause ;;
      10)
        if svc_is_enabled; then
          svc_disable
          echo -e "\n  ${Y}Automatic startup disabled${N}"
        else
          svc_enable
          echo -e "\n  ${G}Automatic startup enabled${N}"
        fi
        pause ;;
      11) do_update; pause ;;
      13) show_links; pause ;;
      12) do_uninstall; pause ;;
      0) exit 0 ;;
      *) ;;
    esac
  done
}

need_root

# With an argument, behave as a regular command instead of opening the menu.
case "${1:-}" in
  start)    svc_start ;;
  stop)     svc_stop ;;
  restart)  svc_restart ;;
  status)   svc_status_page ;;
  log)      svc_logs_follow ;;
  info)     show_info ;;
  list)     list_tunnels ;;
  update)   do_update ;;
  uninstall) do_uninstall ;;
  "")       menu ;;
  *)
    echo "Usage: f [start|stop|restart|status|log|info|list|update|uninstall]"
    echo "Run without arguments to open the interactive menu."
    ;;
esac