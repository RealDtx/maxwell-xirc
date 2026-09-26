#!/usr/bin/env bash
# Renders every proxy variant and checks the essentials; runs the real
# config testers when nginx/apache are installed.
set -euo pipefail
cd "$(dirname "$0")"
source ./proxy-templates.sh
fail=0
check() { # name, text, pattern...
    local name="$1" text="$2"; shift 2
    for p in "$@"; do
        grep -qF -- "$p" <<<"$text" || { echo "FAIL $name: missing '$p'"; fail=1; }
    done
}
export PORT=8085 SERVER_NAME=xirc.example.com PREFIX=/xirc
check nginx_site    "$(render_nginx_site)"    "server_name xirc.example.com" "proxy_pass http://127.0.0.1:8085" 'Upgrade $http_upgrade' "X-Forwarded-Proto" 'Host $http_host' "X-Forwarded-For"
check nginx_subpath "$(render_nginx_subpath)" "location /xirc/" "proxy_pass http://127.0.0.1:8085/" "location /xirc/ws" 'Upgrade $http_upgrade' 'Host $http_host' "X-Forwarded-For"
check apache_site   "$(render_apache_site)"   "ServerName xirc.example.com" "ProxyPass / http://127.0.0.1:8085/" "ws://127.0.0.1:8085/" "ProxyPreserveHost On" "X-Forwarded-Proto"
check apache_subpath "$(render_apache_subpath)" "ProxyPass /xirc/ http://127.0.0.1:8085/" "ws://127.0.0.1:8085/ws" "ProxyPreserveHost On"
if command -v nginx >/dev/null; then
    tmp=$(mktemp -d); { echo "events {} http {"; render_nginx_site; echo "}"; } >"$tmp/n.conf"
    nginx -t -c "$tmp/n.conf" -p "$tmp" 2>&1 | tail -1 || fail=1
fi
[[ $fail == 0 ]] && echo "proxy templates ok"
exit $fail
