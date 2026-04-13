# Frontend CustomResource

You can inject HAProxy `filter` directives into HUG-generated frontends by applying a `Frontend` Custom Resource.

The CRD schema can be found here: [Frontend CRD](../../api/definition/gate.v3.haproxy.org_frontends.yaml)

---

## Overview

The `Frontend` CR is an **overlay** — it does not replace a frontend section, but adds `filter` directives to frontends that HUG generates from Gateway/Listener resources. HUG continues to manage binds, modes, ACLs, and routing rules.

**Primary use case:** injecting a SPOE filter for rate limiting.

**Supported filter types** (from `haproxytech/client-native`):

| Type | HAProxy directive |
|---|---|
| `spoe` | `filter spoe engine <engine> config <file>` |
| `compression` | `filter compression` |
| `cache` | `filter cache <name>` |
| `trace` | `filter trace` |
| `fcgi-app` | `filter fcgi-app <name>` |
| `bwlim-in` | `filter bwlim-in <name> ...` |
| `bwlim-out` | `filter bwlim-out <name> ...` |

---

## Targeting

### Apply to all frontends (nil parentRef)

Omit `spec.parentRef` to inject the FilterList into every HUG-managed frontend:

```yaml
apiVersion: gate.v3.haproxy.org/v3
kind: Frontend
metadata:
  name: global-compression
  namespace: haproxy-unified-gateway
spec:
  filter_list:
    - type: compression
```

### Apply to a specific Gateway

Set `spec.parentRef.name` to the Kubernetes `metadata.name` of the target Gateway.
If `parentRef.namespace` is omitted, it defaults to the namespace of the Frontend CR itself.

```yaml
apiVersion: gate.v3.haproxy.org/v3
kind: Frontend
metadata:
  name: rate-limiting
  namespace: haproxy-unified-gateway
spec:
  parentRef:
    name: hug-gateway          # metadata.name of the Gateway
    namespace: haproxy-unified-gateway
  filter_list:
    - type: spoe
      spoe_config: /etc/haproxy/spoe/rate-limit.conf
      spoe_engine: rate-limit
```

Only the frontends that aggregate listeners owned by `hug-gateway` receive the SPOE filter.
All other frontends remain unchanged.

---

## CRD spec

```
spec:
  parentRef:            # optional — nil targets all frontends
    name: <string>      # metadata.name of the target Gateway (required if parentRef is set)
    namespace: <string> # namespace of the Gateway; defaults to the Frontend CR's namespace
  filter_list:          # list of HAProxy filter directives to inject
    - type: <string>    # required — one of: spoe, compression, cache, trace, fcgi-app, bwlim-in, bwlim-out
      # additional fields depend on type (see client-native models.Filter)
```

---

## Aggregation behaviour

A single HAProxy frontend (`http_80`, `https_443`, …) can aggregate Listeners from multiple
Gateways on the same port. When multiple Frontend CRs match a frontend, their `filter_list`
entries are merged in alphabetical order of the CR name for determinism.

---

## Dynamic updates

Changes to a Frontend CR (create, update, delete) trigger a reconcile. HUG calls
`ReplaceFilters` on the affected frontend, which replaces the complete filter list atomically.
HAProxy is then reloaded gracefully.

Deleting a Frontend CR (or removing all entries from `filter_list`) results in an empty filter
list for the targeted frontend(s).

---

## Example

See [example/customresource/frontend/](../../example/customresource/frontend/) for a complete SPOE rate-limiting example.
