# xirc installer helpers — sourced by install.sh and test-install-lib.sh.
# The functions at the top are pure (no root, no prompts) so CI can test them.

# build_dsn user pass host port db — Go MySQL DSN; IPv6 hosts bracketed.
build_dsn() {
    local host="$3"
    [[ "$host" == *:* && "$host" != \[* ]] && host="[$host]"
    printf '%s:%s@tcp(%s:%s)/%s' "$1" "$2" "$host" "$4" "$5"
}

# env_quote value — single-quoted for .env (Compose takes it literally).
# Fails on values containing a single quote, which can't be expressed.
env_quote() {
    [[ "$1" == *"'"* ]] && { echo "value must not contain a single quote (')" >&2; return 1; }
    printf "'%s'" "$1"
}

# lan_subnets — reads `ip -o -4 route show scope link`, prints "home CIDR" or
# "vpn CIDR" per private subnet; container/VM bridges and down links skipped.
lan_subnets() {
    local cidr dev rest
    while read -r cidr _ dev rest; do
        [[ $cidr == */* && $rest == *"proto kernel"* && $rest != *linkdown* ]] || continue
        case $dev in lo|docker*|br-*|veth*|virbr*|cali*|vxlan*|tunl*|cni*|flannel*|podman*) continue;; esac
        case $cidr in 10.*|192.168.*|172.1[6-9].*|172.2[0-9].*|172.3[01].*) ;; *) continue;; esac
        case $dev in tun*|wg*|tailscale*|zt*) echo "vpn $cidr";; *) echo "home $cidr";; esac
    done
}

# ask_trusted_networks — multiple-choice prompt (uses the caller's ask/say);
# sets TRUSTED_NETWORKS, empty when everyone logs in.
ask_trusted_networks() {
    local home=() vpn=() kind cidr i c other sug
    TRUSTED_NETWORKS=""
    while read -r kind cidr; do
        if [[ $kind == home ]]; then home+=("$cidr"); else vpn+=("$cidr"); fi
    done < <(ip -o -4 route show scope link 2>/dev/null | lan_subnets)
    say "Who may use xirc without logging in?"
    say "  1) Nobody: everyone logs in  [default]"
    for i in "${!home[@]}"; do say "  $((i + 2))) Home network ${home[i]}"; done
    other=$((${#home[@]} + 2))
    say "  $other) Other networks (home, VPN, …: you type them)"
    ask "Choice" "1" c
    if [[ $c =~ ^[0-9]+$ ]] && ((c >= 2 && c < other)); then
        TRUSTED_NETWORKS=${home[c - 2]}
    elif [[ $c == "$other" ]]; then
        local all=("${home[@]}" "${vpn[@]}")
        sug=$(IFS=,; echo "${all[*]}")
        ((${#vpn[@]})) && say "     Detected VPN: ${vpn[*]}"
        say "     Common: 192.168.0.0/16 (any 192.168.x home), 10.0.0.0/8, 172.16.0.0/12,"
        say "             10.8.0.0/24 (OpenVPN), 10.6.0.0/24 (WireGuard/PiVPN), 100.64.0.0/10 (Tailscale)"
        ask "Networks (comma-separated)" "$sug" TRUSTED_NETWORKS
    fi
}

render_env() {
    local k
    echo "# Written by scripts/install.sh — re-run it to change these."
    for k in DOWNLOADS_DIR MEDIA_DIR PUID PGID TZ BIND TRUSTED_NETWORKS TRUSTED_ROLE \
             DB_DRIVER DB_DSN COMPOSE_PROFILES MYSQL_ROOT_PASSWORD MYSQL_PASSWORD XIRC_SERVER_PREFIX; do
        printf '%s=%s\n' "$k" "$(env_quote "${!k-}")"
    done
}

# render_override network — attach xirc to an existing container's network.
render_override() {
    cat <<EOF
# Written by scripts/install.sh: joins the network of an existing DB container.
services:
  xirc:
    networks:
      - xirc
      - dbnet
networks:
  dbnet:
    external: true
    name: $1
EOF
}

# ask_mysql default_host — prompts for an existing MariaDB/MySQL; sets
# DB_HOST DB_PORT DB_NAME DB_USER DB_PASS DB_DSN.
ask_mysql() {
    ask "MariaDB/MySQL host" "$1" DB_HOST
    while true; do
        ask "Port" "3306" DB_PORT
        [[ "$DB_PORT" =~ ^[0-9]+$ ]] && (( DB_PORT >= 1 && DB_PORT <= 65535 )) && break
        echo "  Enter a port number between 1 and 65535." >&2
    done
    ask "Database name" "xirc" DB_NAME
    ask "User" "xirc" DB_USER
    while true; do
        read -rsp "  Password for ${DB_USER}: " DB_PASS; echo >&2
        [[ -n "$DB_PASS" ]] && env_quote "$DB_PASS" >/dev/null && break
        say "! Password must be non-empty and must not contain a single quote."
    done
    DB_DSN="$(build_dsn "$DB_USER" "$DB_PASS" "$DB_HOST" "$DB_PORT" "$DB_NAME")"
    echo
    say "xirc does not create the database. If it doesn't exist yet, run on the DB server:"
    say "  CREATE DATABASE IF NOT EXISTS \`${DB_NAME}\` CHARACTER SET utf8mb4;"
    say "  CREATE USER IF NOT EXISTS '${DB_USER}'@'%' IDENTIFIED BY '<password>';"
    say "  GRANT ALL ON \`${DB_NAME}\`.* TO '${DB_USER}'@'%';"
    echo
}

rand_pw() { openssl rand -hex 16 2>/dev/null || head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n'; }

# min. Compose version: depends_on.required (used in deploy/docker-compose.yaml) needs 2.20+.
MIN_COMPOSE_VERSION=2.20.0

docker_install() {
    local a DIR
    # ── prerequisites
    if ! command -v docker >/dev/null || ! docker compose version >/dev/null 2>&1; then
        say "Docker with the compose plugin is required."
        ask_yn "Install Docker now via the official script (get.docker.com)?" n a
        [[ $a == y ]] || { say "Install Docker (https://docs.docker.com/engine/install/) and re-run."; exit 1; }
        curl -fsSL https://get.docker.com | sh
        docker compose version >/dev/null 2>&1 || { say "! docker compose still unavailable."; exit 1; }
    fi

    local compose_ver
    compose_ver="$(docker compose version --short 2>/dev/null | sed 's/^v//')"
    if [[ "$(printf '%s\n%s\n' "$MIN_COMPOSE_VERSION" "$compose_ver" | sort -V | head -1)" != "$MIN_COMPOSE_VERSION" ]]; then
        say "Docker Compose ${compose_ver:-unknown} is older than the required ${MIN_COMPOSE_VERSION} (needed for depends_on.required)."
        ask_yn "Install a newer Docker now via the official script (get.docker.com)?" n a
        [[ $a == y ]] || { say "Update the compose plugin (https://docs.docker.com/compose/install/) and re-run."; exit 1; }
        curl -fsSL https://get.docker.com | sh
        compose_ver="$(docker compose version --short 2>/dev/null | sed 's/^v//')"
        [[ "$(printf '%s\n%s\n' "$MIN_COMPOSE_VERSION" "$compose_ver" | sort -V | head -1)" == "$MIN_COMPOSE_VERSION" ]] \
            || { say "! docker compose is still older than ${MIN_COMPOSE_VERSION}."; exit 1; }
    fi

    ask "Install directory" "/opt/xirc" DIR
    local reuse=n
    if [[ -f "$DIR/.env" ]]; then
        ask_yn "$DIR/.env exists. Reuse existing settings (just update + restart)?" y reuse
    fi

    if [[ $reuse == n ]]; then
        ask "Downloads directory (files arrive here)" "/srv/downloads" DOWNLOADS_DIR
        ask "Media library (finished downloads are sorted here)" "/srv/media" MEDIA_DIR
        ask "Timezone" "$(cat /etc/timezone 2>/dev/null || echo UTC)" TZ
        ask "Listen address (127.0.0.1 = behind a proxy on this host, 0.0.0.0 = whole LAN)" "127.0.0.1" BIND

        id xirc >/dev/null 2>&1 || { useradd --system --home-dir "$DIR" --shell /usr/sbin/nologin xirc; say "Created system user xirc"; }
        PUID="$(id -u xirc)"; PGID="$(id -g xirc)"
        local d
        for d in "$DIR" "$DIR/data" "$DOWNLOADS_DIR" "$MEDIA_DIR"; do
            [[ -d "$d" ]] || { mkdir -p "$d" && chown xirc:xirc "$d"; }
        done
        runuser -u xirc -- test -w "$DOWNLOADS_DIR" || say "! $DOWNLOADS_DIR is not writable by xirc (uid $PUID) — fix ownership before downloading."
        runuser -u xirc -- test -w "$MEDIA_DIR" || say "! $MEDIA_DIR is not writable by xirc (uid $PUID)."

        echo
        TRUSTED_NETWORKS="" TRUSTED_ROLE=admin
        if [[ $BIND == 127.* || $BIND == ::1 ]]; then
            say "Login: your browser on this machine is admin without login; everyone else logs in."
        else
            ask_trusted_networks
        fi
        [[ -n "$TRUSTED_NETWORKS" ]] && ask "Role for those networks (admin / user)" "admin" TRUSTED_ROLE

        local db net=""
        ask "Database (sqlite / bundled = MariaDB container / existing = your MariaDB/MySQL)" "sqlite" db
        DB_DSN=""; MYSQL_ROOT_PASSWORD=""; MYSQL_PASSWORD=""; COMPOSE_PROFILES=""
        case "${db,,}" in
            b*) DB_DRIVER=mysql; COMPOSE_PROFILES=mariadb
                MYSQL_ROOT_PASSWORD="$(rand_pw)"; MYSQL_PASSWORD="$(rand_pw)"
                DB_DSN="$(build_dsn xirc "$MYSQL_PASSWORD" db 3306 xirc)"
                say "Bundled MariaDB: passwords generated and stored in $DIR/.env";;
            e*) DB_DRIVER=mysql
                local where
                ask "Where does it run? (host = natively on this machine / remote / container = another container here)" "remote" where
                case "${where,,}" in
                    h*) say "! MariaDB must listen on the Docker bridge, not only 127.0.0.1 (bind-address),"
                        say "  and the user needs a grant for the bridge subnet (e.g. 'xirc'@'172.%')."
                        ask_mysql host.docker.internal;;
                    c*) while true; do
                            ask "Docker network of the DB container (docker network ls)" "" net
                            docker network inspect "$net" >/dev/null 2>&1 && break
                            say "! No Docker network named '$net'."
                        done
                        ask_mysql "$(docker network inspect -f '{{range .Containers}}{{.Name}} {{end}}' "$net" | awk '{print $1}')";;
                    *)  ask_mysql "";;
                esac;;
            *)  DB_DRIVER=sqlite;;
        esac

        # Proxy choice before .env: a subpath proxy sets server.prefix.
        PORT=8085
        choose_proxy
        XIRC_SERVER_PREFIX="$PREFIX"

        install -m 0644 "$REPO_DIR/deploy/docker-compose.yaml" "$DIR/docker-compose.yaml"
        ( umask 077; render_env > "$DIR/.env" )
        chmod 0600 "$DIR/.env"; chown root:root "$DIR/.env"
        if [[ -n "$net" ]]; then render_override "$net" > "$DIR/docker-compose.override.yaml"
        else rm -f "$DIR/docker-compose.override.yaml"; fi
        say "Wrote $DIR/docker-compose.yaml and $DIR/.env"
    else
        install -m 0644 "$REPO_DIR/deploy/docker-compose.yaml" "$DIR/docker-compose.yaml"
        say "Updated $DIR/docker-compose.yaml; kept $DIR/.env"
    fi

    # ── start
    say "Pulling and starting…"
    (cd "$DIR" && docker compose pull && docker compose up -d --no-build) || { say "! docker compose failed (see above)."; exit 1; }
    local i status=""
    for i in $(seq 1 45); do
        status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' xirc 2>/dev/null || true)"
        [[ "$status" == healthy ]] && break
        sleep 2
    done
    if [[ "$status" != healthy ]]; then
        say "! xirc did not become healthy (status: ${status:-unknown}). Last log lines:"
        (cd "$DIR" && docker compose logs --tail 50 xirc) | sed 's/^/    /'
        exit 1
    fi
    say "✓ xirc is running"

    # ── admin
    local ADMIN_PW ADMIN_PW2
    ADMIN_CREATED=n
    ask_yn "Create an admin account (or reset an existing admin's password)?" "$([[ $reuse == y ]] && echo n || echo y)" a
    while [[ $a == y ]]; do
        ask "Admin username" "admin" ADMIN_NAME
        read -rsp "  Password (min. 8 characters): " ADMIN_PW; echo >&2
        read -rsp "  Repeat password: " ADMIN_PW2; echo >&2
        if [[ "$ADMIN_PW" != "$ADMIN_PW2" ]]; then say "! Passwords do not match."; continue; fi
        # Password via the environment of docker compose exec, never argv.
        if (cd "$DIR" && XIRC_ADMIN_PASSWORD="$ADMIN_PW" docker compose exec -T -e XIRC_ADMIN_PASSWORD xirc xirc --config /data/config.yaml --create-admin "$ADMIN_NAME" </dev/null); then
            ADMIN_CREATED=y; break
        fi
        ask_yn "! Creating the admin failed (see above). Try again?" y a
    done

    if [[ $reuse == y ]]; then
        say "Updated and restarted. Manage: cd $DIR && docker compose ps | logs -f xirc"
        return
    fi

    # ── proxy (chosen above)
    case "$PROXY" in
        nginx|apache) install_proxy "$PROXY";;
        both) install_proxy nginx; install_proxy apache;;
    esac
    print_summary
    say "Manage: cd $DIR && docker compose ps | logs -f xirc | pull && docker compose up -d --no-build"
}
