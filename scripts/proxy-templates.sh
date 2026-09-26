#!/usr/bin/env bash
# Reverse-proxy config renderers for xirc — the single source for install.sh,
# preconfig.sh and the examples in deploy/. Source this file, set PORT
# (default 8085) plus SERVER_NAME (site) or PREFIX (subpath), call a render_*.
# Subpath variants strip the prefix; set server.prefix to the same value.
#
# Host header: xirc's WebSocket origin check compares Origin's host:port to
# the forwarded Host header, so nginx must forward $http_host (which keeps
# the port), not $host (which drops it).

render_nginx_site() {
    local port="${PORT:-8085}"
    cat <<EOF
# xirc — nginx site. Enable: see proxy_enable_hint / docs/install.md
server {
    listen 80;
    server_name ${SERVER_NAME};

    # Optional: restrict to your LAN (xirc has its own login, so this is extra).
    # allow 192.168.0.0/16;
    # allow 10.0.0.0/8;
    # allow 172.16.0.0/12;
    # deny all;

    location / {
        proxy_pass http://127.0.0.1:${port};
        proxy_set_header Host \$http_host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location /ws {
        proxy_pass http://127.0.0.1:${port};
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$http_host;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_read_timeout 86400;
    }
}
EOF
}

render_nginx_subpath() {
    local port="${PORT:-8085}" p="${PREFIX%/}"
    cat <<EOF
# xirc — nginx locations for an existing server { } block:
#   include /etc/nginx/snippets/xirc.conf;
# Requires server.prefix: ${p} in xirc's config.yaml.
location = ${p} {
    return 301 ${p}/;
}

location ${p}/ {
    proxy_pass http://127.0.0.1:${port}/;
    proxy_set_header Host \$http_host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
}

location ${p}/ws {
    proxy_pass http://127.0.0.1:${port}/ws;
    proxy_http_version 1.1;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host \$http_host;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto \$scheme;
    proxy_read_timeout 86400;
}
EOF
}

render_apache_site() {
    local port="${PORT:-8085}"
    cat <<EOF
# xirc — Apache site. Requires:
#   a2enmod proxy proxy_http proxy_wstunnel rewrite headers
<VirtualHost *:80>
    ServerName ${SERVER_NAME}

    # ProxyPreserveHost forwards the real Host header (xirc's WebSocket
    # origin check needs it); mod_proxy adds X-Forwarded-For automatically.
    ProxyPreserveHost On
    ProxyTimeout 86400
    RequestHeader set X-Forwarded-Proto expr=%{REQUEST_SCHEME}

    RewriteEngine On
    RewriteCond %{HTTP:Upgrade} websocket [NC]
    RewriteCond %{HTTP:Connection} upgrade [NC]
    RewriteRule ^/ws$ ws://127.0.0.1:${port}/ws [P,L]

    ProxyPass / http://127.0.0.1:${port}/
    ProxyPassReverse / http://127.0.0.1:${port}/
</VirtualHost>
EOF
}

render_apache_subpath() {
    local port="${PORT:-8085}" p="${PREFIX%/}"
    cat <<EOF
# xirc — Apache snippet for an existing <VirtualHost> (Include it there).
# Requires: a2enmod proxy proxy_http proxy_wstunnel rewrite headers
# Requires server.prefix: ${p} in xirc's config.yaml.
# ProxyPreserveHost forwards the real Host header (xirc's WebSocket origin
# check needs it); mod_proxy adds X-Forwarded-For automatically.
ProxyPreserveHost On
RequestHeader set X-Forwarded-Proto expr=%{REQUEST_SCHEME}
RedirectMatch 301 ^${p}\$ ${p}/

RewriteEngine On
RewriteCond %{HTTP:Upgrade} websocket [NC]
RewriteCond %{HTTP:Connection} upgrade [NC]
RewriteRule ^${p}/ws\$ ws://127.0.0.1:${port}/ws [P,L]

ProxyPass ${p}/ http://127.0.0.1:${port}/ timeout=86400
ProxyPassReverse ${p}/ http://127.0.0.1:${port}/
EOF
}

# proxy_enable_hint <nginx|apache> <site|subpath> — prints manual enable steps.
proxy_enable_hint() {
    case "$1:$2" in
        nginx:site)    echo "sudo cp xirc.conf /etc/nginx/sites-available/xirc && sudo ln -sf /etc/nginx/sites-available/xirc /etc/nginx/sites-enabled/xirc && sudo nginx -t && sudo systemctl reload nginx" ;;
        nginx:subpath) echo "sudo cp xirc.conf /etc/nginx/snippets/xirc.conf, add 'include /etc/nginx/snippets/xirc.conf;' to your server { } block, then: sudo nginx -t && sudo systemctl reload nginx" ;;
        apache:site)   echo "sudo a2enmod proxy proxy_http proxy_wstunnel rewrite headers && sudo cp xirc.conf /etc/apache2/sites-available/xirc.conf && sudo a2ensite xirc && sudo apachectl configtest && sudo systemctl reload apache2" ;;
        apache:subpath) echo "sudo a2enmod proxy proxy_http proxy_wstunnel rewrite headers && sudo cp xirc.conf /etc/apache2/conf-available/xirc.conf, add 'Include conf-available/xirc.conf' inside your <VirtualHost>, then: sudo apachectl configtest && sudo systemctl reload apache2" ;;
    esac
}
