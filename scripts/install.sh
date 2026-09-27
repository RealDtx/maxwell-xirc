#!/usr/bin/env bash
# xirc installer — run on the target machine:  sudo scripts/install.sh
# Print a proxy config only (no root, no changes):
#   scripts/install.sh --print-proxy apache --server-name xirc.example.com
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
source "$SCRIPT_DIR/proxy-templates.sh"
GITHUB_REPO="RealDtx/maxwell-xirc"

ask() { local p="$1" d="$2" v="$3" i; printf '%s [%s]: ' "$p" "$d" >&2; read -r i; printf -v "$v" '%s' "${i:-$d}"; }
ask_yn() {
    local p="$1" d="$2" v="$3" i
    while true; do
        printf '%s [%s]: ' "$p" "$d" >&2; read -r i; i="${i:-$d}"
        case "${i,,}" in y|yes) printf -v "$v" y; return;; n|no) printf -v "$v" n; return;; esac
    done
}
say() { echo "  $*"; }
hr() { echo "  ──────────────────────────────────────────────────────────"; }

# ── --print-proxy ─────────────────────────────────────────────────────────────
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
    sed -n '2,4p' "$0"; exit 0
fi
if [[ "${1:-}" == "--print-proxy" ]]; then
    kind="${2:-}"; shift 2 || true
    PORT=8085 PREFIX="" SERVER_NAME="xirc.example.com"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --prefix) PREFIX="$2"; shift 2;;
            --server-name) SERVER_NAME="$2"; shift 2;;
            --port) PORT="$2"; shift 2;;
            *) echo "unknown option $1" >&2; exit 2;;
        esac
    done
    mode=site; [[ -n "$PREFIX" ]] && mode=subpath
    [[ "$kind" == nginx || "$kind" == apache ]] || { echo "usage: $0 --print-proxy nginx|apache [--prefix /xirc] [--server-name host] [--port 8085]" >&2; exit 2; }
    export PORT PREFIX SERVER_NAME
    "render_${kind}_${mode}"
    echo; echo "# Enable with:"; echo "#   $(proxy_enable_hint "$kind" "$mode")"
    [[ "$mode" == subpath ]] && echo "#   and set 'server.prefix: ${PREFIX%/}' in xirc's config.yaml"
    exit 0
fi

[[ $EUID -eq 0 ]] || { echo "Run as root: sudo $0" >&2; exit 1; }

echo; say "xirc installer"; hr; say "Press Enter to accept the default in [brackets]."; echo

# ── paths ─────────────────────────────────────────────────────────────────────
ask "Install directory" "/opt/xirc" INSTALL_DIR
ask "Downloads directory (files arrive here)" "/srv/downloads" DOWNLOADS_DIR
ask "Temp directory (in-progress transfers)" "${DOWNLOADS_DIR}/.tmp" TEMP_DIR
ask "Media library (finished downloads are sorted here)" "/srv/media" MEDIA_DIR
ask "Database (sqlite / mysql)" "sqlite" DB_DRIVER
if [[ "${DB_DRIVER,,}" == mysql* || "${DB_DRIVER,,}" == maria* ]]; then
    DB_DRIVER=mysql; DB_PATH=""
    ask "MySQL DSN (user:pass@tcp(host:3306)/db)" "xirc:change-me@tcp(127.0.0.1:3306)/xirc" DB_DSN
else
    DB_DRIVER=sqlite; DB_DSN=""
    ask "SQLite database file" "${INSTALL_DIR}/data/xirc.db" DB_PATH
fi
ask "Port xirc listens on (localhost only)" "8085" PORT

# ── binary ────────────────────────────────────────────────────────────────────
goarch() { case "$(uname -m)" in x86_64) echo amd64;; aarch64|arm64) echo arm64;; armv7l) echo armv7;; *) return 1;; esac; }
need_go() { sed -n 's/^go \([0-9.]*\).*/\1/p' "$REPO_DIR/go.mod"; }
BIN_SRC=""
if [[ -x "$SCRIPT_DIR/xirc" ]]; then BIN_SRC="$SCRIPT_DIR/xirc"
elif [[ -x "$REPO_DIR/xirc" ]]; then BIN_SRC="$REPO_DIR/xirc"
elif command -v go >/dev/null && [[ -f "$REPO_DIR/go.mod" ]] && \
     [[ "$(printf '%s\n%s\n' "$(need_go)" "$(go env GOVERSION | sed 's/^go//')" | sort -V | head -1)" == "$(need_go)" ]]; then
    say "Building from source with $(go version)…"
    (cd "$REPO_DIR" && CGO_ENABLED=0 go build -ldflags="-s -w" -o "$REPO_DIR/xirc" .)
    BIN_SRC="$REPO_DIR/xirc"
elif A="$(goarch)"; then
    say "Downloading the newest release for linux-${A}…"
    URL="$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases" \
        | grep -o "https://[^\"]*/xirc-linux-${A}\"" | head -1 | tr -d '"')" || true
    if [[ -n "$URL" ]]; then
        curl -fsSL -o /tmp/xirc.download "$URL" && chmod +x /tmp/xirc.download && BIN_SRC=/tmp/xirc.download
    fi
fi
if [[ -z "$BIN_SRC" ]]; then
    say "No xirc binary found. Either:"
    say "  - install Go $(need_go)+ and re-run (builds from this checkout), or"
    say "  - build elsewhere: GOOS=linux GOARCH=$(goarch || echo amd64) go build -o xirc . and copy it next to this script."
    exit 1
fi

# ── user + dirs ───────────────────────────────────────────────────────────────
id xirc >/dev/null 2>&1 || { useradd --system --home-dir "$INSTALL_DIR" --shell /usr/sbin/nologin xirc; say "Created system user xirc"; }
install -d -o xirc -g xirc "$INSTALL_DIR" "$INSTALL_DIR/data"
[[ -n "$DB_PATH" ]] && install -d -o xirc -g xirc "$(dirname "$DB_PATH")"
install -m 0755 "$BIN_SRC" "$INSTALL_DIR/xirc"

check_dir() { # $1 dir, $2 label
    local d="$1" label="$2" a
    if [[ ! -d "$d" ]]; then
        ask_yn "$label $d does not exist. Create it (owned by xirc)?" y a
        [[ $a == y ]] && install -d -o xirc -g xirc "$d"
    fi
    [[ -d "$d" ]] || { say "! $label missing — the related feature stays disabled until it exists."; return; }
    if runuser -u xirc -- test -w "$d" -a -r "$d"; then say "✓ $label $d is writable by xirc"; return; fi
    local grp; grp="$(stat -c %G "$d")"
    say "! xirc cannot write to $d (owner $(stat -c '%U:%G %a' "$d"))."
    echo "    1) chown -R xirc:xirc $d"
    echo "    2) add xirc to group '$grp' and make it group-writable (keeps current owner)"
    echo "    3) leave it — xirc will disable the feature and show why"
    ask "Choose" "2" a
    case "$a" in
        1) chown -R xirc:xirc "$d";;
        2) usermod -aG "$grp" xirc && chmod -R g+rwX "$d" && find "$d" -type d -exec chmod g+s {} +;;
        *) say "Left unchanged.";;
    esac
}
check_dir "$DOWNLOADS_DIR" "Downloads dir"
check_dir "$TEMP_DIR" "Temp dir"
check_dir "$MEDIA_DIR" "Media dir"

# ── login ─────────────────────────────────────────────────────────────────────
echo
say "Login: everyone must log in, unless their network is listed here."
ask "Networks that skip login (comma-separated, e.g. 192.168.0.0/16; empty = none)" "" TRUSTED_NETWORKS
TRUSTED_ROLE=admin
[[ -n "$TRUSTED_NETWORKS" ]] && ask "Role for those networks (admin / user)" "admin" TRUSTED_ROLE

# ── proxy choice (before config: subpath sets server.prefix) ──────────────────
HAS_NGINX=n; HAS_APACHE=n
command -v nginx >/dev/null && HAS_NGINX=y
{ command -v apache2ctl >/dev/null || command -v apachectl >/dev/null || command -v httpd >/dev/null; } && HAS_APACHE=y
echo
say "Detected: nginx=$HAS_NGINX apache=$HAS_APACHE"
default_proxy=none; [[ $HAS_APACHE == y ]] && default_proxy=apache; [[ $HAS_NGINX == y ]] && default_proxy=nginx
ask "Reverse proxy to configure (nginx / apache / both / none)" "$default_proxy" PROXY
PROXY="${PROXY,,}"; PREFIX=""; SERVER_NAME=""; MODE=site
if [[ "$PROXY" != none ]]; then
    ask "Own site with a hostname (s) or a subpath of an existing site (p)" "s" m
    if [[ "${m,,}" == p* ]]; then MODE=subpath; ask "URL prefix" "/xirc" PREFIX; PREFIX="${PREFIX%/}"
    else ask "Hostname (e.g. xirc.example.com)" "$(hostname -f 2>/dev/null || hostname)" SERVER_NAME; fi
fi

# ── config + service ──────────────────────────────────────────────────────────
CONFIG="$INSTALL_DIR/config.yaml"
write_config=y
[[ -f "$CONFIG" ]] && ask_yn "$CONFIG exists. Overwrite?" n write_config
if [[ $write_config == y ]]; then
    nets=""; [[ -n "$TRUSTED_NETWORKS" ]] && nets="$(sed 's/ *, */, /g' <<<"$TRUSTED_NETWORKS")"
    {
        echo "server:"; echo "  host: 127.0.0.1"; echo "  port: ${PORT}"
        [[ -n "$PREFIX" ]] && echo "  prefix: ${PREFIX}"
        echo; echo "database:"; echo "  driver: ${DB_DRIVER}"
        if [[ $DB_DRIVER == sqlite ]]; then echo "  path: ${DB_PATH}"; else echo "  dsn: \"${DB_DSN}\""; fi
        cat <<EOF

storage:
  media_dir: ${MEDIA_DIR}
  downloads_dir: ${DOWNLOADS_DIR}
  temp_dir: ${TEMP_DIR}
  min_free_space: 1GB
  critical_free_space: 500MB

auth:
  trusted_networks: [${nets}]
  trusted_role: ${TRUSTED_ROLE}
EOF
    } > "$CONFIG"
    chown xirc:xirc "$CONFIG"; chmod 0640 "$CONFIG"
    say "Wrote $CONFIG"
fi

cat > /etc/systemd/system/xirc.service <<EOF
[Unit]
Description=xirc - XDCC IRC web client
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=xirc
Group=xirc
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/xirc --config ${CONFIG}
Restart=always
RestartSec=5
RestartPreventExitStatus=78
NoNewPrivileges=yes
UMask=0002

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now xirc
systemctl restart xirc
say "Service xirc started (logs: journalctl -u xirc -f)"

# ── proxy install ─────────────────────────────────────────────────────────────
install_proxy() { # $1 nginx|apache
    local kind="$1" conf a
    conf="$(PORT="$PORT" PREFIX="$PREFIX" SERVER_NAME="$SERVER_NAME" "render_${kind}_${MODE}")"
    ask_yn "Install the ${kind} config automatically?" y a
    if [[ $a == y ]]; then
        local target test reload
        case "$kind:$MODE" in
            nginx:site)     target=/etc/nginx/sites-available/xirc; test="nginx -t"; reload="systemctl reload nginx";;
            nginx:subpath)  target=/etc/nginx/snippets/xirc.conf; test="nginx -t"; reload="systemctl reload nginx";;
            apache:site)    target=/etc/apache2/sites-available/xirc.conf; test="apachectl configtest"; reload="systemctl reload apache2";;
            apache:subpath) target=/etc/apache2/conf-available/xirc.conf; test="apachectl configtest"; reload="systemctl reload apache2";;
        esac
        if [[ -d "$(dirname "$target")" ]]; then
            printf '%s\n' "$conf" > "$target"
            if [[ $kind == apache ]]; then a2enmod -q proxy proxy_http proxy_wstunnel rewrite headers; [[ $MODE == site ]] && a2ensite -q xirc; fi
            [[ "$kind:$MODE" == nginx:site ]] && ln -sf "$target" /etc/nginx/sites-enabled/xirc
            if $test >/tmp/xirc-proxy-test.log 2>&1; then
                $reload; say "✓ ${kind} configured"
                [[ $MODE == subpath ]] && say "  Now include it in your site: $(proxy_enable_hint "$kind" subpath | sed 's/.*add //; s/, then.*//')"
                return
            fi
            say "! ${kind} config test failed — reverting:"; sed 's/^/    /' /tmp/xirc-proxy-test.log
            rm -f "$target" /etc/nginx/sites-enabled/xirc
            [[ "$kind:$MODE" == apache:site ]] && a2dissite -q xirc 2>/dev/null || true
        else
            say "! $(dirname "$target") not found (non-Debian layout?)"
        fi
    fi
    echo; say "Manual setup — save this as xirc.conf:"; hr
    printf '%s\n' "$conf"; hr
    say "Then: $(proxy_enable_hint "$kind" "$MODE")"
}
case "$PROXY" in
    nginx|apache) install_proxy "$PROXY";;
    both) install_proxy nginx; install_proxy apache;;
esac

# ── summary ───────────────────────────────────────────────────────────────────
echo; hr
if [[ "$PROXY" == none ]]; then URL="http://127.0.0.1:${PORT}/ (localhost only — add a reverse proxy for other devices)"
elif [[ $MODE == site ]]; then URL="http://${SERVER_NAME}/"
else URL="http://<your-site>${PREFIX}/"; fi
say "Open: $URL"
say "The first visit asks you to create the admin account."
[[ "$PROXY" != none ]] && say "HTTPS: sudo certbot --${PROXY/both/nginx}   (the login cookie is marked Secure over HTTPS)"
say "Re-run this script any time; it asks before changing existing files."
hr
