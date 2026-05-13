# comqtt-dashboard

Web dashboard add-on for [comqtt](https://github.com/wind-c/comqtt), the
lightweight Go MQTT broker. Ships as a separate Go module so the upstream
broker stays focused on the broker itself.

Features:

- **Authentication & Authorization** (v0.3.0): manage MQTT broker users and
  ACL rules through the dashboard against the same backend the broker reads
  (file `auth.Ledger`, redis, mysql, or postgresql). Writes hit the broker's
  store directly, so changes apply on the next `OnConnectAuthenticate` /
  `OnACLCheck` with no restart.
- Pages: Overview, Clients (paginated + search), Subscriptions,
  Topics (collapsible tree), Retained, Sessions, Cluster, Blacklist, Tools
  (publish form), Settings (read-only YAML), Users, Authentication,
  Authorization, Account.
- Cluster-aware REST mirrors (v0.2.0) under `/api/v1/cluster/mqtt/*` that
  fan out to every cluster member, deduplicate, and re-paginate.
- Login + HMAC-signed cookie sessions, file or Redis-backed credential
  store, password rotation on first login, optional password expiry.
- Multi-user dashboard RBAC: `admin` and `viewer` roles. Viewers can browse
  every page and observe state but cannot mutate.
- SSE-driven Recent Events feed on Overview with non-blocking, drop-on-full
  fan-out so the broker hot path can never stall.
- 60-bucket inline-SVG sparklines for message and connection rates.
- Light + dark theme with toggle, persisted in `localStorage`.
- Single binary (~99 MB image, ~40 MB binary) including all assets via
  `go:embed`. No Node toolchain required for builds.

## Position vs upstream's in-tree dashboard

[wind-c/comqtt#159](https://github.com/wind-c/comqtt/pull/159) adds an
in-tree dashboard to upstream comqtt covering Overview, Clients,
Subscriptions, Retained, Nodes, Publish, Auth, ACL, and Login (9 pages,
Redis-only auth/ACL backend). This add-on is broader: 14 pages (adds
Sessions, Topics tree, Blacklist, Settings, Users / dashboard-RBAC,
Account), all four auth backends (file, redis, mysql, postgresql),
cluster-aware REST aggregation, SSE event bus, and per-user RBAC. The two
coexist; pick the one whose surface matches your operations need.

## Status

- `v0.1.0` — single-mode dashboard.
- `v0.2.0` / `v0.2.1` — cluster-mode add-on (StatefulSet, Helm chart, image, CI).
- `v0.3.0` — Authentication + Authorization pages, four auth backends.

## How to run

```bash
go install github.com/debsahu/comqtt-dashboard/cmd/comqtt-dashboard@latest
DASHBOARD_INITIAL_PASSWORD=changeme comqtt-dashboard --storage-way=0
# then open http://localhost:8080/dashboard/  (admin / changeme)
```

The binary is a drop-in replacement for upstream `comqtt-single` on the same
flags. Same broker, same listeners; the dashboard mounts on the existing
`:8080` HTTP listener alongside `/api/v1/*`.

To enable the new Authentication and Authorization pages, configure broker
auth as you normally would (e.g. `--auth-way=1 --auth-ds=0
--auth-path=/data/ledger.yml` for the built-in file ledger). The dashboard
reads `cfg.Auth.*` at startup and selects the matching backend; with
`--auth-way=0` (anonymous) the pages render a "not configured" notice.

## Configuration

The same YAML config the broker reads is also parsed for an optional
top-level `dashboard:` section:

```yaml
dashboard:
  enabled: true              # default
  session-secret: ""         # base64 or raw; auto-generated if empty
  password-expiry-days: 90   # 0 disables
```

Plus `DASHBOARD_INITIAL_PASSWORD` env var to seed the admin password
on first boot. Without it, a random password is printed to stdout.

For broker-side configuration (storage, auth, listeners, TLS, log) refer to
upstream's [`cmd/config/single.yml`](https://github.com/wind-c/comqtt/blob/main/cmd/config/single.yml).

## Module layout

```
dashboard/   The dashboard package (auth, handlers, sse, templates, static)
mqttauth/    MQTT broker user + ACL CRUD backends (file/redis/mysql/postgres)
rest/        Single-node REST endpoints layered on /api/v1/mqtt/*
cluster/rest/ Cluster-aggregating REST mirrors under /api/v1/cluster/mqtt/*
cmd/comqtt-dashboard/         Single-mode broker driver
cmd/comqtt-cluster-dashboard/ Cluster-mode broker driver
deploy/helm/comqtt-dashboard/ Helm chart (Deployment + StatefulSet)
```

## Container image

```
docker pull ghcr.io/debsahu/comqtt-dashboard:0.3.0
```

Multi-arch: `linux/amd64`, `linux/arm64`.

## Helm chart

```
helm repo add comqtt-dashboard https://debsahu.github.io/comqtt-dashboard
helm repo update
helm install my-broker comqtt-dashboard/comqtt-dashboard \
  --set dashboard.initialPassword.value=changeme
```

## Building from source

```bash
git clone https://github.com/debsahu/comqtt-dashboard.git
cd comqtt-dashboard
make dashboard      # rebuild static/tailwind.css from web/input.css (only
                    # needed when editing styles; the committed CSS is
                    # what go:embed picks up)
go build -o ./comqtt-dashboard ./cmd/comqtt-dashboard
```

## Origin

Originally proposed in
[wind-c/comqtt#151](https://github.com/wind-c/comqtt/pull/151). The
maintainer's preference at the time was to keep stock comqtt lightweight, so
the dashboard moved to this separate add-on module. Comqtt later added its
own in-tree dashboard via [#159](https://github.com/wind-c/comqtt/pull/159);
this project's broader feature surface is documented above.

## License

MIT. See [LICENSE](LICENSE).
