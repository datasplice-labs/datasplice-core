## File structure

A Datasplice project is two files: `main.yaml` describes the flow, and `secrets.yaml` describes where credentials come from. A third file, `datasplice.lock`, is generated.

### `main.yaml`

Here is the Zendesk > LLM > CSV flow in full.

```yaml
# main.yaml
name: "Zendesk tickets context and export"

config:
  on_error: "fail"
  mode: "record"

steps:
  # Step 1: import
  # The first step is always the import. It uses the Zendesk package,
  # which describes the Zendesk API but contains no code: the core
  # makes the requests.
  - uses: "github.com/myorg/datasplice-zendesk@v0.2.0"
    # Which of the package's actions to run. The package declares
    # what's available.
    action: "show_many"
    # Settings for this action. The package declares which are
    # required and what types they take.
    with:
      subdomain: "acme"
      resource: "tickets"
    # Secrets this step is allowed to see. Nothing else in the flow
    # can read them, and the package can't ask for more than this.
    secrets:
      - ZENDESK_EMAIL
      - ZENDESK_TOKEN
    # What this step passes to the next one.
    export:
      values:
        id: "in.id"
        status: "in.status"
        subject: "in.subject"
        description: "in.description"
        # The package defines this derived field. From the config's
        # point of view it's just another field on the record.
        contact_reason: "in.contact_reason"

  # Step 2: transform
  - uses: "github.com/third-party-org/datasplice-anthropic@v5.22.6"
    action: "context"
    # This step overrides the global mode: 100 records per API call
    # instead of one call per record.
    mode: "bulk"
    bulk_size: 100
    with:
      model: "claude-opus-5"
      prompt: "Translate subject and description. Provide context."
      # Which fields from the incoming record the action uses.
      values:
        subject: "in.subject"
        description: "in.description"
    secrets:
      - ANTHROPIC_TOKEN
    export:
      # Keep every field this step received.
      passthrough: true
      # And add what this step produced.
      values:
        context: "out.context"

  # Step 3: export
  # A built-in package. No version, it's pinned to the core's.
  - uses: "datasplice/csv"
    with:
      # Relative to main.yaml. Created if missing, along with folders.
      output: "tickets.csv"
      # Write to a temp file and rename on success, so a failed run
      # never leaves a half-written file.
      atomic: true
    # No export block. The CSV package writes whatever it receives:
    # every Zendesk field plus `context`. Column order comes from the
    # first record.
```

### Expressions

Field mapping uses **dotted paths and nothing else**. No function calls, no arithmetic, no conditionals.

Two scopes:

| Scope | Means |
|---|---|
| `in` | The record this step received |
| `out` | What this step's action produced |

`in` is available in every step. `out` is only available in `export.values`, and only for steps that produce something (a transform action's response, for instance). An import step has no `out`: it *is* the `out`.

Paths traverse nesting: `in.requester.name`. Array indexing (`in.items.0.sku`) is not in v0.

If you need a field that isn't a straight path (a custom field pulled out of an array, a value that needs digging for) that belongs in the **package manifest** as a derived field, not in `main.yaml`. The package declares it, and from the config it looks like any other field.

### `export`

Every step may declare what it passes forward.

```yaml
export:
  passthrough: false      # default: only `values` are passed on
  values:
    new_name: "in.old_name"
```

`passthrough: true` forwards every field the step received, and `values` adds to or overrides them.

A step with no `export` block passes its records through unchanged.

### `config`

```yaml
config:
  # What happens when a step fails on a record.
  #
  #   fail         Stop the whole flow immediately. Nothing further
  #                is processed. (default)
  #
  #   skip_record  Drop that record and carry on with the rest.
  #                A summary of skipped records prints at the end.
  #
  on_error: "fail"

  # How each step receives records.
  #
  #   record       One at a time, in order. (default)
  #   bulk         In batches of `bulk_size`.
  #   concurrent   `concurrency` records in flight at once.
  #
  mode: "record"

  bulk_size: 100        # only for mode: bulk
  concurrency: 10       # only for mode: concurrent

  # error | warn | info | debug. Each level includes the ones above it.
  log_level: "info"
```

`mode`, `bulk_size`, and `concurrency` can be set per step, which overrides the global value. This matters: in the example above, Zendesk pages 100 at a time, the LLM step wants batches to save cost, and the CSV step writes row by row.

`on_error` is global only. Mixing failure semantics between steps makes the outcome of a partial run impossible to reason about.

#### On the word "transactional"

A failing record isn't passed to the next step. That's all `on_error` guarantees.

It does **not** mean writes are rolled back. If record 50 fails after 49 rows are already in a CSV, those rows exist. Real atomicity is a property some sinks can offer: `atomic: true` on the CSV package writes to a temp file and renames at the end, so the output file is either complete or absent. Database and API sinks won't generally have an equivalent. Check each package's docs.

### `secrets.yaml`

Secrets are declared per step in `main.yaml`, and `secrets.yaml` says where the values come from.

```yaml
# secrets.yaml
secrets:
  type: "file"
  filepath: "./.env"
```

Four types:

```yaml
# Read KEY=value pairs from a dotenv file.
type: "file"
filepath: "./.env"
```

```yaml
# Values inline. Convenient locally; gitignore this file.
# The core prints a warning every time it loads one.
type: "plain"
values:
  ANTHROPIC_TOKEN: "sk_1234"
  ZENDESK_EMAIL: "me@company.org"
  ZENDESK_TOKEN: "abc123=="
```

```yaml
# Already in the process environment. This is the CI, Kubernetes,
# Vault, and Cloud Run path.
type: "injected"
```

```yaml
# This flow uses no secrets. Explicit, so a missing block is
# always an error rather than a silent empty set.
type: "noenv"
```

The `secrets` block may live in `main.yaml` instead, for small flows. **If it appears in both files, that's an error.** Silent precedence between two files is how you end up running against the wrong environment without noticing.

#### Least privilege

Secrets are never put into the environment wholesale. The core reads them, holds them privately, and gives each step only the ones it listed under `secrets:`.

In the example, the Anthropic step cannot see `ZENDESK_TOKEN`. It isn't in its list, so it never reaches it.

Two supporting rules:

- A package manifest **declares** which secrets it needs. If a step's `secrets:` list doesn't cover them, the flow fails at `validate`, before anything runs.
- Every value the core prints (logs, errors, plan output) is passed through a redactor that replaces known secret values with `***`.

### `datasplice.lock`

Generated by `datasplice get`. Pins every package to a version and a content hash.

```
version 1

github.com/myorg/datasplice-zendesk v0.2.0 sha256:5rXk2N8vQm...
github.com/third-party-org/datasplice-anthropic v5.22.6 sha256:9qLm4Kp1Xz...
```

Commit this. Without it, the same config can resolve to a different manifest on a different machine, and "it's in git so it's reproducible" stops being true.

`@latest` is allowed for built-in packages, which are pinned to the core's version anyway. It's rejected for third-party ones.

### Commands

```
datasplice get        Download and cache manifests, write the lockfile
datasplice validate   Check config and manifests. No network, no data
datasplice plan       Show the resolved flow: steps, actions, secrets,
                      destination. Still no data fetched
datasplice run        Execute
```

`validate` and `plan` are fast and offline, which makes them worth running in CI on every pull request.

### What a project looks like

```
my-flow/
├── main.yaml
├── secrets.yaml        # gitignored if type is "plain"
├── .env                # gitignored
└── datasplice.lock     # committed
```
