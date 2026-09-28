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

# docker_install — temporary stub; Task 3 replaces this with the real Docker flow.
docker_install() { say "Docker mode: not implemented yet"; exit 1; }
