#!/usr/bin/env bash
# xirc deployment preconfig — generates a named profile directory with all
# deployment artefacts: config.yaml, xirc.service, nginx config, settings.mk.
# Run with: bash scripts/preconfig.sh   or   make preconfig [PROFILE=name]
set -euo pipefail

# ── helpers ───────────────────────────────────────────────────────────────────

ask() {
    local prompt="$1" default="$2" var_name="$3"
    local input
    printf '%s [%s]: ' "$prompt" "$default" >&2
    read -r input
    printf -v "$var_name" '%s' "${input:-$default}"
}

ask_yn() {
    local prompt="$1" default="$2" var_name="$3"
    local input
    while true; do
        printf '%s [%s]: ' "$prompt" "$default" >&2
        read -r input
        input="${input:-$default}"
        case "${input,,}" in
            y|yes) printf -v "$var_name" 'y'; return ;;
            n|no)  printf -v "$var_name" 'n'; return ;;
            *)     echo "  Please enter y or n." >&2 ;;
        esac
    done
}

hr() { echo "  ──────────────────────────────────────────────────────────"; }

# ── welcome ───────────────────────────────────────────────────────────────────

echo
echo "  xirc deployment preconfig"
hr
echo "  Press Enter to accept the default shown in [brackets]."
echo

# ── profile name ─────────────────────────────────────────────────────────────

ask "Target server name (profile name)" "${PROFILE:-maxwell}" PROFILE
PROFILE_DIR=".${PROFILE}"

if [[ -d "$PROFILE_DIR" ]]; then
    echo
    echo "  Note: ${PROFILE_DIR}/ already exists — files will be overwritten."
fi

# ── deployment target ─────────────────────────────────────────────────────────

echo
echo "  -- Deployment target --"
ask "SSH host (IP or hostname of the Pi)" "192.168.20.2" PI_HOST
ask "SSH user (must have passwordless sudo)" "pi" PI_USER

# ── installation ──────────────────────────────────────────────────────────────

echo
echo "  -- Installation --"
ask "Install directory on target" "/opt/xirc" INSTALL_DIR
ask "Service OS username" "xirc" XIRC_USER
ask "HTTP port xirc listens on" "8085" XIRC_PORT

# ── storage ───────────────────────────────────────────────────────────────────

echo
echo "  -- Storage directories (paths on the target server) --"
ask "Downloads directory" "/srv/downloads" DOWNLOADS_DIR
ask "DLNA / media directory" "/srv/dlna/media" MEDIA_DIR
ask "Temp directory (for in-progress DCC transfers)" "${DOWNLOADS_DIR}/.tmp" TEMP_DIR

# ── database ──────────────────────────────────────────────────────────────────

echo
echo "  -- Database --"
echo "  xirc creates all tables automatically on first startup (CREATE TABLE IF NOT EXISTS)."
echo "  No manual schema setup is required."
echo
echo "  Options: sqlite  mysql"
ask "Database driver" "sqlite" DB_DRIVER

case "${DB_DRIVER,,}" in
    sqlite)
        DB_DRIVER="sqlite"
        ask "SQLite file path (on target)" "${INSTALL_DIR}/data/xirc.db" DB_PATH
        DB_DSN=""
        ;;
    mysql|mariadb)
        DB_DRIVER="mysql"
        DB_PATH=""
        echo
        echo "  DSN format: user:password@tcp(host:3306)/dbname"
        echo "  The database and user must already exist; tables are created automatically."
        ask "MySQL DSN" "xirc:changeme@tcp(127.0.0.1:3306)/xirc" DB_DSN
        ;;
    *)
        echo "  Unknown driver '${DB_DRIVER}' — defaulting to sqlite."
        DB_DRIVER="sqlite"
        ask "SQLite file path (on target)" "${INSTALL_DIR}/data/xirc.db" DB_PATH
        DB_DSN=""
        ;;
esac

# ── DCC ───────────────────────────────────────────────────────────────────────

echo
echo "  -- DCC (XDCC transfer settings) --"
ask_yn "Enable passive DCC (needed if Pi is behind NAT without port forwarding)" "n" DCC_PASSIVE
DCC_PASSIVE_BOOL="false"
[[ "$DCC_PASSIVE" == "y" ]] && DCC_PASSIVE_BOOL="true"

DCC_EXTERNAL_IP=""
if [[ "$DCC_PASSIVE" == "y" ]]; then
    ask "External IP for passive DCC (public IP of the Pi / router)" "" DCC_EXTERNAL_IP
fi

# ── nginx ─────────────────────────────────────────────────────────────────────

echo
ask_yn "Generate nginx reverse proxy config" "y" USE_NGINX

NGINX_MODE=""
NGINX_SERVER_NAME=""
NGINX_LISTEN_PORT=""
NGINX_PREFIX=""

if [[ "$USE_NGINX" == "y" ]]; then
    echo
    echo "  Nginx mode:"
    echo "    s) Standalone  — new server block with its own server_name"
    echo "    i) Subpath     — location blocks to include in an existing server"
    echo
    ask "Mode" "i" NGINX_MODE
    case "${NGINX_MODE,,}" in
        s|standalone)
            NGINX_MODE="standalone"
            ask "Nginx server_name (hostname for the vhost)" "xirc.local" NGINX_SERVER_NAME
            ask "Nginx listen port" "80" NGINX_LISTEN_PORT
            NGINX_PREFIX=""
            ;;
        *)
            NGINX_MODE="subpath"
            ask "URL prefix (no trailing slash)" "/xirc" NGINX_PREFIX
            NGINX_LISTEN_PORT=""
            NGINX_SERVER_NAME=""
            ;;
    esac
else
    echo
    echo "  *** IMPORTANT SECURITY WARNING ***"
    echo "  Nginx is disabled.  xirc will listen on 127.0.0.1:${XIRC_PORT}."
    echo "  Do NOT expose port ${XIRC_PORT} directly to the internet."
    echo "  You MUST place xirc behind a TLS-terminating reverse proxy before"
    echo "  any public access.  Without a proxy, the WebSocket and session"
    echo "  cookies are transmitted in plaintext."
    echo
fi

# ── generate files ────────────────────────────────────────────────────────────

mkdir -p "$PROFILE_DIR"

# --- settings.mk (read by Makefile) ---
cat > "${PROFILE_DIR}/settings.mk" <<SETTINGS
# Generated by scripts/preconfig.sh — do not edit by hand; re-run preconfig.
PI_HOST     := ${PI_HOST}
PI_USER     := ${PI_USER}
INSTALL_DIR := ${INSTALL_DIR}
XIRC_USER   := ${XIRC_USER}
SETTINGS

# --- config.yaml ---
if [[ "$DB_DRIVER" == "sqlite" ]]; then
    DB_BLOCK="database:
  driver: sqlite
  path: ${DB_PATH}"
else
    DB_BLOCK="database:
  driver: mysql
  dsn: \"${DB_DSN}\""
fi

PREFIX_LINE=""
[[ -n "$NGINX_PREFIX" ]] && PREFIX_LINE="  prefix: ${NGINX_PREFIX}"

cat > "${PROFILE_DIR}/config.yaml" <<CONFIG
server:
  host: 127.0.0.1
  port: ${XIRC_PORT}
${PREFIX_LINE}

${DB_BLOCK}

storage:
  media_dir: ${MEDIA_DIR}
  downloads_dir: ${DOWNLOADS_DIR}
  temp_dir: ${TEMP_DIR}
  min_free_space: 1GB
  critical_free_space: 500MB
  auto_extract:
    enabled: false
    delete_archive: false

dcc:
  passive_enabled: ${DCC_PASSIVE_BOOL}
  passive_ports: "30000-30010"
  external_ip: "${DCC_EXTERNAL_IP}"

downloads:
  max_concurrent: 3

notifications:
  quiet_hours_start: ""
  quiet_hours_end: ""
CONFIG

# --- xirc.service ---
cat > "${PROFILE_DIR}/xirc.service" <<SERVICE
[Unit]
Description=xirc - XDCC IRC Web Client
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=${XIRC_USER}
Group=${XIRC_USER}
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/xirc --config ${INSTALL_DIR}/config.yaml
Restart=always
RestartSec=5
StartLimitIntervalSec=120
StartLimitBurst=5
RestartPreventExitStatus=78
StandardOutput=journal
StandardError=journal

# NoNewPrivileges is universally supported; the namespace-based options
# (ProtectSystem=strict, ProtectHome, PrivateTmp) are omitted because
# they require mount namespaces which may not be available on all kernels.
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
SERVICE

# --- nginx-xirc.conf (or sentinel) ---
if [[ "$USE_NGINX" == "y" && "$NGINX_MODE" == "standalone" ]]; then
    cat > "${PROFILE_DIR}/nginx-xirc.conf" <<NGINX
server {
    listen ${NGINX_LISTEN_PORT};
    server_name ${NGINX_SERVER_NAME};

    # Restrict to internal networks only
    allow 192.168.0.0/16;
    allow 10.0.0.0/8;
    allow 172.16.0.0/12;
    deny all;

    location / {
        proxy_pass http://127.0.0.1:${XIRC_PORT};
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location /ws {
        proxy_pass http://127.0.0.1:${XIRC_PORT};
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_read_timeout 86400;
    }
}
NGINX
    rm -f "${PROFILE_DIR}/.no-nginx"
    rm -f "${PROFILE_DIR}/nginx-xirc-location.conf"

elif [[ "$USE_NGINX" == "y" && "$NGINX_MODE" == "subpath" ]]; then
    cat > "${PROFILE_DIR}/nginx-xirc-location.conf" <<NGINX
# xirc location blocks — include this inside your existing nginx server block:
#   include /etc/nginx/snippets/xirc.conf;

# Redirect bare prefix to trailing slash
location = ${NGINX_PREFIX} {
    return 301 ${NGINX_PREFIX}/;
}

# Main proxy (nginx strips the prefix before forwarding)
location ${NGINX_PREFIX}/ {
    allow 192.168.0.0/16;
    allow 10.0.0.0/8;
    allow 172.16.0.0/12;
    deny all;

    proxy_pass http://127.0.0.1:${XIRC_PORT}/;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
}

# WebSocket (must be separate — needs upgrade headers)
location ${NGINX_PREFIX}/ws {
    allow 192.168.0.0/16;
    allow 10.0.0.0/8;
    allow 172.16.0.0/12;
    deny all;

    proxy_pass http://127.0.0.1:${XIRC_PORT}/ws;
    proxy_http_version 1.1;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_read_timeout 86400;
}
NGINX
    rm -f "${PROFILE_DIR}/.no-nginx"
    rm -f "${PROFILE_DIR}/nginx-xirc.conf"

else
    touch "${PROFILE_DIR}/.no-nginx"
    rm -f "${PROFILE_DIR}/nginx-xirc.conf"
    rm -f "${PROFILE_DIR}/nginx-xirc-location.conf"
fi

# --- .gitignore ---
GITIGNORE=".gitignore"
GI_PATTERN=".${PROFILE}/"
if ! grep -qxF "$GI_PATTERN" "$GITIGNORE" 2>/dev/null; then
    echo "$GI_PATTERN" >> "$GITIGNORE"
    echo "  Added '${GI_PATTERN}' to .gitignore"
fi

# ── summary ───────────────────────────────────────────────────────────────────

echo
hr
echo "  Generated: ${PROFILE_DIR}/"
echo "    config.yaml       application config"
echo "    xirc.service      systemd unit"
if [[ "$USE_NGINX" == "y" && "$NGINX_MODE" == "standalone" ]]; then
    echo "    nginx-xirc.conf          nginx vhost config"
elif [[ "$USE_NGINX" == "y" && "$NGINX_MODE" == "subpath" ]]; then
    echo "    nginx-xirc-location.conf nginx location snippet (include in your server block)"
fi
echo "    settings.mk       Makefile deploy variables"
hr
echo
echo "  First deploy (pushes config + binary + service files):"
echo "    make deploy PROFILE=${PROFILE}"
echo
echo "  For first-time Pi setup, follow: docs/install.md"
echo
