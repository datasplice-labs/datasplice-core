# Datasplice — PRD & Task Breakdown

*The manifest model. A core that reads YAML and moves data, and packages that are descriptions rather than programs.*

Companion documents: [what-is-datasplice.md](./what-is-datasplice.md), [file-structure.md](./file-structure.md), [manifest-spec.md](./manifest-spec.md).

---

## 1. What we're building

A single Go binary. It reads `main.yaml`, fetches data from a source, applies steps to it, and writes it somewhere. The steps that talk to third-party APIs are described by manifest files, which the core interprets — no third-party code ever runs.

## 2. Why the manifest model

Three properties fall out of it, and together they're the reason to build this rather than use Benthos or write a script:

- **Installing a package is safe.** A description can't read your environment or open a socket. Nothing else in this space can say that.
- **Writing a package doesn't need a compiler.** It's YAML plus knowledge of an API.
- **Retries, rate limits, and pagination are written once.** A package author can't get them wrong because they don't write them.

## 3. Two kinds of step, and the difference matters

This wasn't explicit in the other documents and it should be:

| | Built-in package | Manifest package |
|---|---|---|
| Examples | `datasplice/csv`, `datasplice/json`, `datasplice/map` | `datasplice-zendesk`, `datasplice-anthropic` |
| What it is | Go code inside the core | A YAML file on GitHub |
| Handles | Local files, in-memory reshaping | HTTP APIs |
| Versioning | Pinned to the core | Own Git tags |

Manifests describe HTTP APIs and nothing else. Anything that isn't an HTTP API — reading a CSV, writing JSON, renaming fields — is Go code in the core. That boundary keeps the manifest format small.

## 4. v0 scope

**In:**
- CSV and JSON source and sink
- Field mapping with dotted paths, `passthrough`, rename
- Manifest packages with `source` actions over HTTP
- Bearer and basic auth
- Cursor and page pagination
- Rate limiting, retry with `Retry-After`
- Per-step secret scoping, four secret types, redaction
- Package resolution from GitHub tags, content-hash lockfile
- `validate`, `plan`, `get`, `run`
- One flow mode: records processed one at a time, in order, failing the flow on error

**Out (each is additive later):**
- `transform` and `sink` actions in manifests
- `bulk` and `concurrent` modes
- `on_error: skip_record`
- Incremental state and cursors
- Signed auth (SigV4, HMAC)
- WASM hooks
- Sinks beyond local files

**Definition of done for v0:** a real Zendesk manifest, tagged on GitHub, pulls real tickets into a CSV on your machine, from a config you'd be happy to commit.

## 5. Repo layout

```
datasplice/
├── cmd/datasplice/main.go
├── internal/
│   ├── config/       # main.yaml + secrets.yaml parsing and validation
│   ├── secrets/      # four types, scoping, redaction
│   ├── record/       # Record type, dotted paths
│   ├── manifest/     # manifest parsing, validation, templating
│   ├── httpengine/   # request build, auth, pagination, rate limit, retry
│   ├── registry/     # fetch, cache, lockfile
│   ├── builtin/      # csv, json, map
│   └── pipeline/     # step wiring, export evaluation, the runner
├── testdata/
└── examples/
```

---

# Tasks

Each task is meant to be one sitting. Each has a **done when** so there's no ambiguity about finishing it.

## P0 — Skeleton

Half a day. Gets you a binary that does nothing, which is worth more than it sounds.

- [ ] **T0.1** Create the repo, `go mod init`, `.gitignore`, Apache-2.0 licence.
  *Done when:* `go build ./...` succeeds.
- [ ] **T0.2** Cobra root command with `--version` and `--help`.
  *Done when:* `datasplice --version` prints a version set via ldflags.
- [ ] **T0.3** Stub subcommands: `validate`, `plan`, `get`, `run`. Each prints "not implemented" and exits 1.
  *Done when:* all four are listed in `--help`.
- [ ] **T0.4** GitHub Actions: `go test ./...`, `go vet ./...`, `golangci-lint`.
  *Done when:* CI is green on the first push.
- [ ] **T0.5** Makefile with `build`, `test`, `lint`, `install`.
  *Done when:* `make test` runs.

## P1 — Config parsing

1–2 days. No data moves yet; this is about failing clearly on bad input.

- [ ] **T1.1** Go structs for `main.yaml`: `Flow`, `Config`, `Step`, `Export`.
  *Done when:* a valid example unmarshals into them.
- [ ] **T1.2** YAML loader using `yaml.v3` with `KnownFields(true)`.
  *Done when:* a config with an unknown top-level key errors.
- [ ] **T1.3** Error type carrying file, line, and column.
  *Done when:* an unknown key error reads `main.yaml:14:3: unknown key "slect"`.
- [ ] **T1.4** Validate flow-level required fields: `name` present, `steps` non-empty.
  *Done when:* an empty `steps:` list errors with a clear message.
- [ ] **T1.5** Validate step shape: `uses` required; `action`, `with`, `secrets`, `export`, `mode` optional.
  *Done when:* a step missing `uses` errors and names the step index.
- [ ] **T1.6** Parse `uses` into `{host, path, version}`. Accept `datasplice/<name>` for built-ins.
  *Done when:* table test covers valid refs, missing `@version`, and malformed paths.
- [ ] **T1.7** Validate the `config:` block: known keys only, `on_error` and `mode` from their allowed sets.
  *Done when:* `on_error: "acid"` errors and lists the valid values.
- [ ] **T1.8** Wire `validate` to load and check, exiting 0 or 1.
  *Done when:* `datasplice validate` works on a good and a bad file.
- [ ] **T1.9** Golden tests: `testdata/config/valid/*.yaml` and `invalid/*.yaml` with expected error strings.
  *Done when:* at least 10 invalid cases are covered.

## P2 — Secrets

1 day. Get this right early; it's hard to retrofit.

- [ ] **T2.1** Structs and loader for `secrets.yaml`.
  *Done when:* all four `type` values parse.
- [ ] **T2.2** `type: plain` — read `values` from the file. Print a warning when used.
  *Done when:* values load and the warning appears on stderr.
- [ ] **T2.3** `type: file` — parse a dotenv file at `filepath`.
  *Done when:* `KEY=value`, quoted values, blank lines, and `#` comments all handled.
- [ ] **T2.4** `type: injected` — read from `os.Environ()`.
  *Done when:* an env var set before running is available to a step.
- [ ] **T2.5** `type: noenv` — empty set, no error.
  *Done when:* a flow with no secrets validates.
- [ ] **T2.6** Error if `secrets` appears in both `main.yaml` and `secrets.yaml`.
  *Done when:* the error names both files.
- [ ] **T2.7** Error if `secrets` appears in neither.
  *Done when:* the message suggests `type: noenv`.
- [ ] **T2.8** Per-step scoping: build a map containing only the names in that step's `secrets:` list.
  *Done when:* a unit test shows step B cannot see step A's secret.
- [ ] **T2.9** Error if a step lists a secret that isn't defined.
  *Done when:* the message names the step and the missing key.
- [ ] **T2.10** Redactor: an `io.Writer` wrapper replacing known secret values with `***`.
  *Done when:* a string containing a secret is masked on write.
- [ ] **T2.11** Route all stdout and stderr output through the redactor at the top level.
  *Done when:* no code path writes to the terminal without passing through it.
- [ ] **T2.12** Test: run a flow at `log_level: debug` and assert no secret value appears in captured output.
  *Done when:* the test passes and fails if the redactor is removed.

## P3 — Records and the local pipeline

2–3 days. Ends with a binary that does something real.

- [ ] **T3.1** `Record` type: `map[string]any` over the JSON model.
  *Done when:* the type and its doc comment exist.
- [ ] **T3.2** `Get(path)` with dotted traversal.
  *Done when:* `in.requester.name` resolves through nested maps; missing paths return `false`.
- [ ] **T3.3** `Set`, `Delete`, `Has`, `Keys`, `Clone`.
  *Done when:* `Set("a.b", v)` creates intermediate maps; setting through a non-map errors.
- [ ] **T3.4** Typed accessors: `String`, `Number`, `Bool`, each returning `(value, ok)`.
  *Done when:* wrong-typed fields return `false` rather than panicking.
- [ ] **T3.5** Record test suite: nesting, missing paths, type mismatches, empty records, unicode keys.
  *Done when:* ~20 cases pass.
- [ ] **T3.6** Internal interfaces: `Source`, `Transform`, `Sink`.
  *Done when:* defined, with `Source.Read(ctx, emit func(Record) error) error`.
- [ ] **T3.7** CSV source: read a file, first row as headers, every value a string.
  *Done when:* a 3-column file produces records with 3 string fields.
- [ ] **T3.8** CSV sink: header from the first record's key order; missing keys become empty cells.
  *Done when:* records with differing keys produce a valid file.
- [ ] **T3.9** JSON source: read an array, or an object at a configured path.
  *Done when:* both shapes load.
- [ ] **T3.10** JSON sink: write an array of records.
  *Done when:* output is valid JSON and round-trips through the JSON source.
- [ ] **T3.11** Evaluate `export.values` with the `in` scope.
  *Done when:* `subject: "in.subject"` maps correctly and an unknown path errors at validate.
- [ ] **T3.12** `passthrough: true` merges incoming fields, with `values` overriding.
  *Done when:* a test shows override order.
- [ ] **T3.13** Pipeline runner: source → transforms → sink, one record at a time, fail on error.
  *Done when:* a 3-step flow runs to completion.
- [ ] **T3.14** `run` command wired to the runner, with a summary line at the end.
  *Done when:* `datasplice run` prints `✓ 142 records in 0.4s`.
- [ ] **T3.15** `atomic: true` for file sinks — temp file, rename on success, delete on failure.
  *Done when:* a mid-run failure leaves no output file.
- [ ] **T3.16** End-to-end test: CSV in, renamed fields, JSON out.
  *Done when:* it runs in CI with no network.

> **Milestone 1.** A binary that converts a CSV to a JSON with different keys. Small, but complete and shippable.

## P4 — Manifest parsing

2 days. Still no network.

- [ ] **T4.1** Manifest structs: `name`, `version`, `manifest_version`, `settings`, `secrets`, `base_url`, `auth`, `rate_limit`, `retry`, `actions`.
  *Done when:* the Zendesk example from the spec unmarshals.
- [ ] **T4.2** Strict parsing; reject unknown keys with file and line.
  *Done when:* a typo'd key errors.
- [ ] **T4.3** Reject `manifest_version` values the core doesn't support.
  *Done when:* `manifest_version: 2` errors and names the supported range.
- [ ] **T4.4** Reject reserved keys `hook` and `state`.
  *Done when:* using either errors with "reserved for a future version".
- [ ] **T4.5** Validate `settings` specs: `type` in the allowed set, `one_of` only on strings.
  *Done when:* a malformed spec errors.
- [ ] **T4.6** Validate a step's `with:` against the manifest's `settings`: required present, types right, `one_of` respected.
  *Done when:* a missing required setting errors and names it.
- [ ] **T4.7** Template engine for `{{ settings.x }}`.
  *Done when:* substitution works; an unknown reference errors rather than producing an empty string.
- [ ] **T4.8** Add `{{ secrets.X }}`, permitted only in `auth` and `headers`.
  *Done when:* using it in `path` or `query` errors, naming the restriction.
- [ ] **T4.9** Check a step's `secrets:` list covers every secret the manifest declares.
  *Done when:* an uncovered secret errors at validate.
- [ ] **T4.10** `validate --manifest <file>` for package authors.
  *Done when:* it checks a manifest standalone, without a `main.yaml`.
- [ ] **T4.11** Golden tests: `testdata/manifest/valid/` and `invalid/`.
  *Done when:* at least 10 invalid cases are covered.

## P5 — The HTTP engine

3–4 days. The biggest chunk, and the part that makes manifests worth having.

- [ ] **T5.1** Build a request from `base_url` + `path` + `query`, with templating applied.
  *Done when:* the Zendesk example produces the expected URL.
- [ ] **T5.2** `auth: bearer`.
  *Done when:* the `Authorization` header is set correctly.
- [ ] **T5.3** `auth: basic`.
  *Done when:* the header is base64-encoded correctly, including the `email/token` form.
- [ ] **T5.4** `auth: header` and `auth: query`.
  *Done when:* both place the value where expected.
- [ ] **T5.5** `auth: none`.
  *Done when:* no auth header is set.
- [ ] **T5.6** Pick a JSONPath library and wrap it behind an internal interface.
  *Done when:* `$.tickets`, `$.data.items`, and `$.content[0].text` all evaluate.
- [ ] **T5.7** Extract records using the `records` path; error clearly if it isn't an array.
  *Done when:* a non-array target errors with the actual type found.
- [ ] **T5.8** `fields:` — evaluate each path against the record and attach the result.
  *Done when:* a filtered path like `$.custom_fields[?(@.id==360001)].value` produces a field.
- [ ] **T5.9** Wire a `source` action to the `Source` interface.
  *Done when:* a manifest-described endpoint emits records into the pipeline.
- [ ] **T5.10** `paginate: cursor` — follow `next` until `until` is satisfied.
  *Done when:* a 3-page `httptest` server yields all records.
- [ ] **T5.11** `paginate: page` — increment a param until an empty result.
  *Done when:* the same test passes with page-number pagination.
- [ ] **T5.12** `until: empty` and the single comparison form (`$.x == false`).
  *Done when:* both terminate correctly, and anything more complex errors at validate.
- [ ] **T5.13** Rate limiter: token bucket from `rate_limit`, with per-action overrides.
  *Done when:* a test shows requests are spaced as configured.
- [ ] **T5.14** Retry on the configured status codes, honouring `Retry-After`.
  *Done when:* a server returning `429` with `Retry-After: 1` is retried after ~1s.
- [ ] **T5.15** Exponential backoff with jitter when there's no `Retry-After`.
  *Done when:* delays grow and are not identical across attempts.
- [ ] **T5.16** Give up after `max_attempts` with an error naming the URL, status, and records emitted so far.
  *Done when:* the message tells you where it stopped.
- [ ] **T5.17** Respect context cancellation inside retry and rate-limit waits.
  *Done when:* `Ctrl-C` during a backoff exits within a second.
- [ ] **T5.18** `max_records` step option to cap a run.
  *Done when:* `max_records: 50` stops after 50, mid-page.

## P6 — Package resolution

1–2 days.

- [ ] **T6.1** Resolve a ref to a manifest URL for a GitHub tag.
  *Done when:* `github.com/org/repo@v0.2.0` produces the right raw URL.
- [ ] **T6.2** Fetch and cache under `~/.datasplice/packages/<host>/<path>/<version>/`.
  *Done when:* a second `get` doesn't re-download.
- [ ] **T6.3** `DATASPLICE_HOME` override.
  *Done when:* setting it relocates the cache.
- [ ] **T6.4** Hash the manifest (SHA-256) and write `datasplice.lock`.
  *Done when:* the lockfile matches the documented format.
- [ ] **T6.5** Verify the hash at `validate` and `run`; fail on mismatch.
  *Done when:* editing a cached manifest causes a clear failure.
- [ ] **T6.6** Reject `@latest` for third-party refs; allow it for `datasplice/*`.
  *Done when:* both cases behave as specified.
- [ ] **T6.7** `get` command: resolve every step, write the lockfile, report what changed.
  *Done when:* it prints added and updated packages.
- [ ] **T6.8** Fail `validate` and `run` if a ref is missing from the lockfile, suggesting `datasplice get`.
  *Done when:* the message is actionable.
- [ ] **T6.9** Clear errors for: repo not found, tag not found, no `datasplice.yaml` at the tag.
  *Done when:* all three are distinguishable, and the tag case lists available tags.

## P7 — plan

Half a day.

- [ ] **T7.1** Print the resolved flow: step number, name, role, package ref, action.
  *Done when:* output matches the example in the config doc.
- [ ] **T7.2** Show each step's destination or endpoint.
  *Done when:* a source shows its URL template and a sink shows its output path.
- [ ] **T7.3** Show the secret names each step will receive — names only, never values.
  *Done when:* verified against the redaction test.
- [ ] **T7.4** Print check results: roles compose, lockfile complete, secrets resolve.
  *Done when:* each line shows ✓ or ✗.
- [ ] **T7.5** Exit non-zero if any check fails.
  *Done when:* it's usable as a CI gate.

## P8 — The real thing

1 day. This is the milestone that tells you whether any of it works.

- [ ] **T8.1** Write `datasplice.yaml` for Zendesk: tickets, basic auth, cursor pagination, one derived field.
  *Done when:* `datasplice validate --manifest` passes.
- [ ] **T8.2** Test it against a real Zendesk account with `max_records: 10`.
  *Done when:* 10 real tickets reach a CSV.
- [ ] **T8.3** Push the manifest repo, tag `v0.1.0`.
  *Done when:* `datasplice get` resolves it from GitHub.
- [ ] **T8.4** Run the full flow: Zendesk → CSV, no record cap.
  *Done when:* it completes and the row count matches Zendesk's UI.
- [ ] **T8.5** README for the manifest repo: settings, required secrets, how to get a token, an example `main.yaml`.
  *Done when:* someone else could use it without asking you anything.
- [ ] **T8.6** Write down every friction point hit while building T8.1.
  *Done when:* the list exists. Each item is a manifest-format fix.

> **Milestone 2 — v0 done.** Real data, real API, real manifest fetched from a tag, reproducible from a lockfile.

## P9 — Release

1 week, and only worth doing once you've run your own flow for a couple of weeks.

- [ ] **T9.1** goreleaser config, cross-platform builds.
- [ ] **T9.2** GitHub release workflow on tag.
- [ ] **T9.3** Homebrew tap.
- [ ] **T9.4** `docs/` — getting started, config reference, manifest reference.
- [ ] **T9.5** `examples/` — three working flows.
- [ ] **T9.6** A `datasplice-manifest-template` repo.

---

## After v0

Roughly in the order the need will show up, based on what the Zendesk flow will be missing:

1. **`transform` actions** — the Anthropic step from the example config. Needs `body` templating with the `in` scope, plus `output:` mapping to `out.*`.
2. **`bulk` mode** — needed the moment a transform calls a paid API. Includes `batch.max` negotiation.
3. **Incremental state** — the `state` reserved key. A cursor persisted between runs, plus `datasplice state reset`. Note this also needs a non-local backend before it's useful in CI, since a fresh runner has no `~/.datasplice`.
4. **`on_error: skip_record`** — the first time one bad record kills a 10,000-record run.
5. **`sink` actions** — writing back to an API.
6. **Signed auth** — SigV4 and HMAC as first-party types.
7. **WASM hooks** — only when a real API defeats the declarative format.

Each is additive. None of them invalidates a manifest already written, which is the point of the reserved keys.

## Estimate

| Phase | Days |
|---|---|
| P0 Skeleton | 0.5 |
| P1 Config | 1.5 |
| P2 Secrets | 1 |
| P3 Records + pipeline | 2.5 |
| P4 Manifest | 2 |
| P5 HTTP engine | 3.5 |
| P6 Resolution | 1.5 |
| P7 plan | 0.5 |
| P8 Zendesk | 1 |
| **v0 total** | **~14 focused days** |

Part-time, that's six to eight weeks. Milestone 1 lands at day 5, which matters — a working binary that early is what keeps the project alive.

## Risks

- **P5 is half the work.** If it slips, the honest fallback is to ship Milestone 1 as its own thing (a local CSV/JSON transformer) and treat HTTP as v0.2.
- **JSONPath libraries vary in quality**, particularly on filter expressions. Evaluate two before committing, and keep the interface wrapper from T5.6 so switching is cheap.
- **The Zendesk manifest will expose format gaps.** Expect a breaking `manifest_version` bump after T8.6. Better now, while you're the only author.
