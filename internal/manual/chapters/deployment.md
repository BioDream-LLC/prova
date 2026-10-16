# Deployment and Operation

## On a server

Copy the binary to the machine, give it a certificate, and run it as an unprivileged user:

```
prova -addr :8443 -tls-cert /etc/prova/cert.pem -tls-key /etc/prova/key.pem \
      -data /var/lib/prova -http-redirect :8080
```

`-http-redirect :8080` answers plain HTTP on that address with a permanent redirect to HTTPS, so a person who types the address without `https://` still arrives.

A systemd unit:

```
[Unit]
Description=prova
After=network-online.target

[Service]
User=prova
ExecStart=/usr/local/bin/prova -addr :8443 -tls-cert /etc/prova/cert.pem -tls-key /etc/prova/key.pem -data /var/lib/prova
Restart=on-failure
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/prova

[Install]
WantedBy=multi-user.target
```

## Certificates

prova serves HTTPS only, TLS 1.2 and later. With `-tls-cert` and `-tls-key` it uses the certificate given — the right choice for anything shared. Without them it generates a self-signed ECDSA certificate once, keeps it in its configuration directory, and prints the fingerprint at start so it can be checked when the browser asks.

## Health

`GET /api/health` answers `{"status":"ok"}` without signing in, for a load balancer or a monitor. `GET /api/version` gives the version.

## Upgrading

Stop prova, replace the binary, start it. The database schema is upgraded in place on start; an older binary refuses to open a database a newer one has upgraded rather than misreading it.

An earlier prova that kept accounts in `users.json` has them imported into the database on first start, after which the file is renamed `users.json.imported`.

## The prova-proof companion

The optional C++ companion re-checks every proof with a SAT solver. Build it with `make proof` (against CaDiCaL when `~/cadical` or `CADICAL=` points at a built copy) and put `prova-proof` beside `prova` or on the `PATH`; prova finds and uses it, and the certificate records which engine signed. Without it, prova does the same checks in-process.
