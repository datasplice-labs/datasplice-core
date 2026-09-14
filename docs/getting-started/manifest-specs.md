## Manifest spec

A package is a single file, `datasplice.yaml`, in the root of a Git repository. It describes an API. It contains no code and is never executed: the core reads it and makes the requests itself.

This document is the format.

### A complete example

```yaml
# datasplice.yaml
# github.com/myorg/datasplice-zendesk

name: "zendesk"
version: "0.2.0"
description: "Zendesk Support API"
manifest_version: 1

# Settings
# What a step may pass in `with:`. Validated before anything runs.
settings:
  subdomain:
    type: string
    required: true
    description: "Your Zendesk subdomain, e.g. 'acme' for acme.zendesk.com"
  resource:
    type: string
    required: true
    one_of: [tickets, users, organizations]

# Secrets
# What this package needs. A step must list all of these in its own
# `secrets:` block or the flow fails at validate.
secrets:
  ZENDESK_EMAIL:
    description: "Email of the API user"
  ZENDESK_TOKEN:
    description: "API token from Admin → Apps and integrations → APIs"

# Connection
base_url: "https://{{ settings.subdomain }}.zendesk.com/api/v2"

auth:
  type: basic
  username: "{{ secrets.ZENDESK_EMAIL }}/token"
  password: "{{ secrets.ZENDESK_TOKEN }}"

rate_limit:
  requests: 200
  per: minute

retry:
  on: [429, 500, 502, 503, 504]
  respect_retry_after: true
  max_attempts: 5
  backoff: exponential

# Actions
actions:
  show_many:
    role: source
    method: GET
    path: "/{{ settings.resource }}.json"
    query:
      "page[size]": 100

    # Where the records are in the response body.
    records: "$.{{ settings.resource }}"

    paginate:
      style: cursor
      next: "$.links.next"
      until: "$.meta.has_more == false"

    # Fields the core computes and attaches to each record, on top of
    # whatever the API returned. This is where logic that would
    # otherwise need code lives.
    fields:
      contact_reason: "$.custom_fields[?(@.id==360001)].value"
```

A step then uses it:

```yaml
- uses: "github.com/myorg/datasplice-zendesk@v0.2.0"
  action: "show_many"
  with:
    subdomain: "acme"
    resource: "tickets"
  secrets: [ZENDESK_EMAIL, ZENDESK_TOKEN]
```

### Templating

`{{ ... }}` substitutes values into strings. Three scopes:

| Scope | Available in | Means |
|---|---|---|
| `settings` | anywhere | values from the step's `with:` block |
| `secrets` | `auth` and headers only | values the step was granted |
| `in` | transform actions only | the incoming record |

`secrets` is restricted to `auth` and `headers` on purpose. A secret should never end up in a URL path, a query string, or a log line, and the format is what enforces that rather than the author's discipline.

Substitution is literal — no expressions, no arithmetic, no conditionals.

### JSONPath

`records`, `fields`, and `output` use JSONPath to point into the response body. The common forms:

```
$.tickets                                  an array at the top level
$.data.items                               nested
$.content[0].text                          an array element
$.custom_fields[?(@.id==360001)].value     a filtered array element
```

### Action roles

Every action declares one.

**`source`**: produces records. Makes requests, follows pagination, emits each element of `records`. Always the first step in a flow.

**`transform`**: receives records, produces fields. Called per record or per batch depending on the step's `mode`. The response maps to fields via `output:`, which become available as `out.*` in the step's `export` block.

```yaml
actions:
  context:
    role: transform
    method: POST
    path: "/v1/messages"
    body:
      model: "{{ settings.model }}"
      max_tokens: 1024
      messages:
        - role: "user"
          content: "{{ settings.prompt }}\n\n{{ in.subject }}\n{{ in.description }}"
    output:
      context: "$.content[0].text"
```

**`sink`**: receives records and writes them somewhere. Sends a request per record or per batch and produces nothing downstream. Always the last step.

```yaml
actions:
  create_many:
    role: sink
    method: POST
    path: "/tickets/create_many.json"
    body:
      tickets: "{{ in }}"
```

### Batching

Transform and sink actions can declare how they want records. The step can override it, but the manifest sets a sensible default and a hard limit.

```yaml
actions:
  context:
    role: transform
    batch:
      default: 1
      max: 100
```

If a step asks for `bulk_size: 500` and the manifest says `max: 100`, that's a validation error rather than a request the API will reject at run time.

### Pagination

Four styles cover nearly everything.

```yaml
# Follow a URL or token from the response.
paginate:
  style: cursor
  next: "$.links.next"
  until: "$.meta.has_more == false"
```

```yaml
# Increment a page number until an empty result.
paginate:
  style: page
  param: "page"
  start: 1
  until: empty
```

```yaml
# Increment an offset by the page size.
paginate:
  style: offset
  param: "offset"
  size_param: "limit"
  size: 100
  until: empty
```

```yaml
# Parse the standard Link header.
paginate:
  style: link_header
```

Omit `paginate` entirely for endpoints that return everything in one response.

### Auth

```yaml
auth: { type: basic, username: "...", password: "..." }
auth: { type: bearer, token: "{{ secrets.TOKEN }}" }
auth: { type: header, name: "X-API-Key", value: "{{ secrets.KEY }}" }
auth: { type: query, name: "api_key", value: "{{ secrets.KEY }}" }
auth: { type: none }
```

Signed auth (AWS SigV4, Shopify HMAC) can't be expressed as a template because it requires hashing the request. These are being added as first-party types in the core, since there are only a handful that matter:

```yaml
auth:
  type: aws_sigv4
  region: "{{ settings.region }}"
  service: "s3"
  access_key: "{{ secrets.AWS_ACCESS_KEY_ID }}"
  secret_key: "{{ secrets.AWS_SECRET_ACCESS_KEY }}"
```

### Rate limiting and retries

Both are enforced by the core, which is the point of declaring them.

```yaml
rate_limit:
  requests: 200
  per: minute        # second | minute | hour

  # Some APIs have a different, much lower budget on certain
  # endpoints. Override per action.
  overrides:
    incremental_export:
      requests: 10
      per: minute
```

```yaml
retry:
  on: [429, 500, 502, 503, 504]
  respect_retry_after: true   # honour the header rather than guessing
  max_attempts: 5
  backoff: exponential        # exponential | linear | fixed
  initial_delay: 1s
```

`respect_retry_after: true` should be the default for any API that sends the header. Guessing a shorter delay than the server asked for gets you rate limited harder.

### Reserved keys

Two keys are reserved in `manifest_version: 1` and rejected if used. They exist so the format doesn't have to break later:

- **`hook`**: a sandboxed WASM module for transformations that can't be expressed declaratively.
- **`state`**: per-step persistent storage, needed for incremental sources that remember a cursor between runs.

### Publishing

A manifest repo is a Git repo with `datasplice.yaml` at the root. A version is a tag:

```bash
git tag v0.2.0
git push origin v0.2.0
```

That's the whole release process. No registry, no account, no build step.

Two rules:

- **Never move a published tag.** Users have the old content hash in their lockfiles, and moving a tag turns a working flow into a hash mismatch. Ship a new version instead.
- **`v0.x` means unstable.** Stay there while the manifest is still changing shape.

Suggested repo layout:

```
datasplice-zendesk/
├── datasplice.yaml
├── README.md            # what it does, required settings, how to get a token
├── examples/
│   └── tickets-to-csv/
│       └── main.yaml
└── .github/workflows/validate.yml
```

The validate workflow should run `datasplice validate --manifest datasplice.yaml` on every push, so a broken manifest is caught before it's tagged.

### What the core does with a manifest

On `datasplice get`:

1. Fetch `datasplice.yaml` at the given tag.
2. Check `manifest_version` is supported.
3. Parse and validate against this spec; reject unknown keys.
4. Hash the file, write the hash to `datasplice.lock`.
5. Cache it under `~/.datasplice/packages/`.

On `datasplice validate`:

1. For every step, load the cached manifest and verify its hash.
2. Check `action` exists and its role fits the step's position.
3. Check `with:` against `settings` — required keys present, types right, `one_of` respected.
4. Check the step's `secrets:` covers everything the manifest declares.
5. Check `bulk_size` against the action's `batch.max`.
6. Check every `in.*` path in `export.values` against what the previous step exports.

All of it offline.

### Scope for v0

Building the whole format at once is a lot. A reasonable first cut:

- `settings` with `string`, `number`, `bool`, `required`, `one_of`
- `secrets` declaration
- `base_url`, `auth` with `bearer` and `basic`
- one `source` action: `method`, `path`, `query`, `records`
- `paginate` with `cursor` and `page`
- `retry` and `rate_limit`

That's enough for a real Zendesk package. `transform` actions, `sink` actions, `fields`, batching, and the remaining auth types come after — each one is additive and won't break manifests already written.
