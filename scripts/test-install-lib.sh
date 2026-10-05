#!/usr/bin/env bash
# Tests the pure helpers in install-lib.sh; with docker available, also
# validates the rendered .env against deploy/docker-compose.yaml.
set -euo pipefail
cd "$(dirname "$0")"
source ./install-lib.sh
fail=0
eq() { [[ "$2" == "$3" ]] || { echo "FAIL $1: got '$2' want '$3'"; fail=1; }; }

eq dsn_v4 "$(build_dsn xirc 'p@ss' 10.0.0.5 3306 xirc)" 'xirc:p@ss@tcp(10.0.0.5:3306)/xirc'
eq dsn_v6 "$(build_dsn xirc pw fd00::5 3307 db)" 'xirc:pw@tcp([fd00::5]:3307)/db'
eq dsn_name "$(build_dsn u p host.docker.internal 3306 x)" 'u:p@tcp(host.docker.internal:3306)/x'
eq quote "$(env_quote 'a$b #c "d"')" "'a\$b #c \"d\"'"
routes='10.244.0.7 dev cali0123456789a
172.17.0.0/16 dev docker0 proto kernel src 172.17.0.1 linkdown
172.18.0.0/16 dev br-0123456789ab proto kernel src 172.18.0.1
192.168.20.0/24 dev eth0 proto kernel src 192.168.20.5 metric 600
192.168.20.0/24 dev wlan0 proto kernel src 192.168.20.7 metric 700
192.168.122.0/24 dev virbr0 proto kernel src 192.168.122.1
10.8.0.0/24 dev tun0 proto kernel src 10.8.0.1
84.1.2.0/24 dev wan0 proto kernel src 84.1.2.3'
eq lan_subnets "$(lan_subnets <<<"$routes" | sort -u | tr '\n' ' ')" "home 192.168.20.0/24 vpn 10.8.0.0/24 "
env_quote "it's" >/dev/null 2>&1 && { echo "FAIL quote: accepted a single quote"; fail=1; }

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
render_case() { # name — uses the caller's variables
    DOWNLOADS_DIR=/srv/dl MEDIA_DIR=/srv/media PUID=1001 PGID=1001 TZ=UTC BIND=127.0.0.1 \
    TRUSTED_NETWORKS="" TRUSTED_ROLE=admin render_env >"$tmp/$1.env"
    grep -q "^DB_DRIVER='$DB_DRIVER'$" "$tmp/$1.env" || { echo "FAIL $1: DB_DRIVER"; fail=1; }
}
DB_DRIVER=sqlite DB_DSN="" MYSQL_ROOT_PASSWORD="" MYSQL_PASSWORD="" COMPOSE_PROFILES="" render_case sqlite
DB_DRIVER=mysql DB_DSN='xirc:pw@tcp(db:3306)/xirc' MYSQL_ROOT_PASSWORD=r MYSQL_PASSWORD=pw COMPOSE_PROFILES=mariadb render_case bundled
DB_DRIVER=mysql DB_DSN='xirc:pw@tcp(10.0.0.5:3306)/xirc' MYSQL_ROOT_PASSWORD="" MYSQL_PASSWORD="" COMPOSE_PROFILES="" render_case remote
DB_DRIVER=mysql DB_DSN='xirc:pw@tcp(mariadb:3306)/xirc' MYSQL_ROOT_PASSWORD="" MYSQL_PASSWORD="" COMPOSE_PROFILES="" render_case container
render_override my_db_net >"$tmp/override.yaml"
grep -q "name: my_db_net" "$tmp/override.yaml" || { echo "FAIL override: network name"; fail=1; }

if command -v docker >/dev/null && docker compose version >/dev/null 2>&1; then
    compose=../deploy/docker-compose.yaml
    for c in sqlite bundled remote; do
        docker compose --env-file "$tmp/$c.env" -f "$compose" config -q || { echo "FAIL compose config $c"; fail=1; }
    done
    docker compose --env-file "$tmp/container.env" -f "$compose" -f "$tmp/override.yaml" config -q || { echo "FAIL compose config container"; fail=1; }
    docker compose --env-file "$tmp/bundled.env" -f "$compose" config | grep -q "XIRC_DATABASE_DSN: xirc:pw@tcp(db:3306)/xirc" || { echo "FAIL bundled DSN not passed through"; fail=1; }
else
    echo "skip: docker compose not installed"
fi
[[ $fail == 0 ]] && echo "install-lib: ok"
exit $fail
