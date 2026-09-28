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
