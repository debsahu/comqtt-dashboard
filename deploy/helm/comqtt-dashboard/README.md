# comqtt-dashboard Helm chart

Deploys the comqtt MQTT broker with the dashboard add-on pre-wired, in
either single mode (`Deployment`) or cluster mode (`StatefulSet` with
Raft + Gossip + Redis).

## Quick start

### Single mode

```bash
helm install my-broker deploy/helm/comqtt-dashboard \
  --set image.tag=0.2.0 \
  --set dashboard.initialPassword.value=changeme
kubectl port-forward svc/my-broker-comqtt-dashboard 8080:8080
open http://localhost:8080/dashboard/   # log in as admin / changeme
```

### Cluster mode (3 nodes)

Requires a RESP-compatible store (Redis or Valkey) reachable from the pods.

```bash
helm install my-broker deploy/helm/comqtt-dashboard \
  --set mode=cluster \
  --set image.tag=0.2.0 \
  --set config.redis.options.addr=valkey.default.svc.cluster.local:6379 \
  --set dashboard.initialPassword.value=changeme
```

## Key values

| Key | Default | Description |
|---|---|---|
| `mode` | `single` | `single` (Deployment) or `cluster` (StatefulSet). |
| `replicaCount` | `3` | Cluster-mode only. Must be odd (Raft quorum). |
| `image.repository` | `ghcr.io/debsahu/comqtt-dashboard` | Image bundling both broker binaries + dashboard assets. |
| `image.tag` | `""` (tracks `appVersion`) | Pin a real version; `latest` is rejected by the chart. |
| `dashboard.passwordExpiryDays` | `90` | Force password rotation cadence. `0` disables. |
| `dashboard.sessionSecret.value` | `""` | Inline HMAC secret (base64 or raw). Rendered into a chart-managed Secret. Optional. |
| `dashboard.initialPassword.value` | `""` | Inline initial admin password. If empty, a random one is printed to stdout on first boot. |
| `dashboard.sessionSecret.existingSecret` | `""` | Reference an externally-managed Secret instead of `.value`. |
| `dashboard.initialPassword.existingSecret` | `""` | Same, for the initial password. |
| `gateway.enabled` | `false` | Gateway API (HTTPRoute + TCPRoute). Preferred over Ingress. |
| `ingress.enabled` | `false` | Deprecated; prefer `gateway.*`. |

For all values, see [`values.yaml`](values.yaml).

## Notes

- **The dashboard is always served.** There is no `dashboard.enabled` toggle:
  this chart's image is the add-on. If you want stock comqtt without the
  dashboard, use the upstream `wind-c/comqtt` chart.
- **Cluster mode requires Redis/Valkey** (`config.storage-way: 3`) for both
  session storage and the dashboard's cross-node event bridge.
- **No bundled Redis sub-chart.** Bitnami images now require authentication,
  so the chart leaves the store to you. See [`ci/valkey.yaml`](ci/valkey.yaml)
  for a minimal in-cluster Valkey example.
