# comqtt-dashboard

Web dashboard add-on for [comqtt](https://github.com/wind-c/comqtt), the
lightweight Go MQTT broker. Ships as a separate Go module so the upstream
broker stays focused on the broker itself.

Features:

- Login + HMAC-signed cookie sessions, file or Redis-backed credential store,
  password rotation on first login, optional password expiry.
- Multi-user RBAC: `admin` and `viewer` roles. Viewers can browse every page
  and observe state but cannot mutate.
- Pages: Overview, Clients (paginated + search), Subscriptions,
  Topics (collapsible tree), Retained, Sessions, Cluster, Blacklist, Tools
  (publish form), Settings (read-only YAML), Users, Account.
- SSE-driven Recent Events feed on Overview with non-blocking, drop-on-full
  fan-out so the broker hot path can never stall.
- 60-bucket inline-SVG sparklines for message and connection rates.
- Light + dark theme with toggle, persisted in `localStorage`.
- Single binary (~40 MB) including all assets via `go:embed`. No Node toolchain
  required for builds.

## Status

`v0.1.0` ships single-mode only. Cluster-mode wiring (cross-node event
aggregation, cluster mirror REST endpoints, multi-node user store, helm chart)
lands in `v0.2.0`.

## How to run

```bash
go install github.com/debsahu/comqtt-dashboard/cmd/comqtt-dashboard@latest
DASHBOARD_INITIAL_PASSWORD=changeme comqtt-dashboard --storage-way=0
# then open http://localhost:8080/dashboard/  (admin / changeme)
```

The binary is a drop-in replacement for upstream `comqtt-single` on the same
flags. Same broker, same listeners; the dashboard mounts on the existing
`:8080` HTTP listener alongside `/api/v1/*`.

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
rest/        Dashboard-specific REST endpoints layered on top of upstream
             /api/v1/mqtt/* (paginated client list, subscriptions, topics,
             retained, sessions, plus DELETE for unsubscribe / clear-retained
             / disconnect-session)
cmd/comqtt-dashboard/   Single-mode broker driver wiring upstream comqtt
                        + the dashboard
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

## Upstream dependency

Until [wind-c/comqtt#158](https://github.com/wind-c/comqtt/pull/158) (the
`Server.Hooks()` accessor) merges and a release is cut, this module
soft-forks comqtt at the same commit pushed in that PR via a `replace`
directive in `go.mod`. The replace will be dropped once an upstream release
includes the accessor.

## Origin

Originally proposed in
[wind-c/comqtt#151](https://github.com/wind-c/comqtt/pull/151). The
maintainer's preference was to keep stock comqtt lightweight, so the
dashboard moved to this separate add-on module.

## License

MIT. See [LICENSE](LICENSE).
