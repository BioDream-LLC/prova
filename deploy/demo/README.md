# prova.perfuse.health:9443

The public demonstration of prova. It runs against prova's own simulated targets only; nothing in it is real.

| | |
|---|---|
| Console | `https://prova.perfuse.health:9443/`, name `demo`, password `try-prova-now` (look at everything, change nothing) |
| Manual | `https://prova.perfuse.health:9443/manual` |
| Health | `https://prova.perfuse.health:9443/api/health` |

`-demo-traffic 15m` runs one shipped scenario every fifteen minutes, so the dashboard and reports show fresh verdicts;
`-retain-days 7` keeps a week of history. The administrator's password is printed once to the service's journal on
first start (`journalctl -u prova-demo`).

Install or upgrade with `sh deploy/demo/provision.sh` from a checkout, with the AWS CLI signed in.
