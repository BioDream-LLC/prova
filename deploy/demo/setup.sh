#!/bin/sh
# setup.sh - run on the demonstration instance as root (provision.sh copies this directory there and runs it).
# Installs the binary, a Let's Encrypt certificate for prova.perfuse.health and the service. Safe to run again.
set -eu
host=prova.perfuse.health
here=$(cd "$(dirname "$0")" && pwd)

apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl certbot >/dev/null
id prova >/dev/null 2>&1 || useradd --system --home /opt/prova --shell /usr/sbin/nologin prova
install -d -o prova -g prova -m 0700 /opt/prova/data
install -d -o root -g prova -m 0750 /opt/prova/tls
install -m 0755 "$here/prova" /opt/prova/prova

# Let's Encrypt on port 80, which none of the services on this machine listen on.
if [ ! -d "/etc/letsencrypt/live/$host" ]; then
  certbot certonly --standalone --non-interactive --agree-tos --register-unsafely-without-email -d "$host"
fi
cat > /etc/letsencrypt/renewal-hooks/deploy/prova.sh <<HOOK
#!/bin/sh
install -o root -g prova -m 0640 /etc/letsencrypt/live/$host/fullchain.pem /etc/letsencrypt/live/$host/privkey.pem /opt/prova/tls/
systemctl restart prova-demo
HOOK
chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/prova.sh
install -o root -g prova -m 0640 "/etc/letsencrypt/live/$host/fullchain.pem" "/etc/letsencrypt/live/$host/privkey.pem" /opt/prova/tls/

install -m 0644 "$here/prova-demo.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now prova-demo
systemctl restart prova-demo
sleep 3
curl -fsS -o /dev/null -w "%{http_code}" "https://$host:9443/api/health" --resolve "$host:9443:127.0.0.1" && echo " - prova is up on https://$host:9443"
