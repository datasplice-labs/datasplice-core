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

This one is format-only: it describes an API you'd need an account for.
See `http-source/` for a manifest you can actually run.

## `http-source/`

A manifest package running against a real API (the free
[JSONPlaceholder](https://jsonplaceholder.typicode.com)): it pages through
posts 10 at a time, rate limited and with retries, stops after 25 records
(`max_records`), and writes three fields to `posts.csv`. Needs network access.


```bash
cd examples/http-source
datasplice validate   # offline: checks the flow and the manifest's settings
datasplice run
```

You can also run
```bash
go run ./main.go validate --manifest examples/http-source/datasplice.yaml
cd examples/http-source
go run ./../../main.go run
```

`uses: "./datasplice.yaml"` points at a manifest on disk, which is how you
try a package before publishing it. Fetching published packages
(`github.com/org/datasplice-x@v1.0.0`) isn't built yet.

## `http-builtin/`

The same JSONPlaceholder posts flow as `http-source/`, but with no
`datasplice.yaml` at all — `datasplice/http` is a one-endpoint source,
everything (`url`, `query`, `records`, `paginate`, `max_records`) goes
straight in the step's `with:` block. It's the same engine underneath
(see CLAUDE.md "Manifest packages"), just without a manifest file for a
one-off endpoint. Needs network access.

```bash
cd examples/http-builtin
datasplice validate
datasplice run
```

You can also run
```bash
go run ./main.go validate --manifest examples/http-builtin/datasplice.yaml
cd examples/http-builtin
go run ./../../main.go run
```