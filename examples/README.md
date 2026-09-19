# Examples

## `csv-export/`

A real, runnable flow using only built-in packages: reads `data.json`,
keeps three fields, writes `out.csv`. Try it:

```bash
cd examples/csv-export
datasplice run
```

If you haven't installed Datasplice, run:
```bash
cd examples/csv-export
go run ..\..\main.go run
```

## `manifest/`

`datasplice.yaml` for a hypothetical Zendesk package — demonstrates the
manifest format (settings, secrets, auth, rate limiting, retry, actions).
Validates standalone:

```bash
datasplice validate --manifest examples/manifest/datasplice.yaml
```

You can also run
```bash
go run ./main.go validate --manifest path-to-manifest/datasplice.yaml
```

Not runnable in a flow yet: fetching third-party manifests and executing
their actions against a real API aren't built yet.
