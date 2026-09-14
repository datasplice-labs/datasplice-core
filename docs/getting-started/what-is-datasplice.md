## What is Datasplice

Datasplice is a data transformation tool. You write a YAML file describing where data comes from, what happens to it, and where it goes. Datasplice runs it.

A flow has three parts:

1. **Input**: a CSV file, a JSON file, or an HTTP API.
2. **Transformation**: reshaping records, renaming fields, changing types, or sending them through another API and keeping the result.
3. **Output**: a file, a spreadsheet, a database table, or an upload to something like S3.

The simplest useful flow imports a CSV and exports a JSON with different keys. A more interesting one pulls tickets from Zendesk, sends each through an LLM for translation, and writes the result to a CSV. There is no ceiling here. Every flow is tailored to your needs.

### The core

The core is the engine. It reads the config, fetches the data, applies the steps, and writes the output. Everything that touches your data happens here: HTTP requests, authentication, retries, rate limiting, pagination, file writing.

This is deliberate. Those are the parts that are easy to get wrong and tedious to rewrite, so they get written once.

### Packages are descriptions, not programs

The core knows how to make HTTP requests, but it doesn't know anything about, for example, Zendesk. Or Linear. Or Notion. Or whatever you need. That's what a package provides.

**A package is a YAML file that describes APIs.** It says what the base URL is, how authentication works, which endpoints exist, where the records live in the response, and how pagination works. It contains no code and it is never executed. The core reads it and makes the calls itself.

A useful way to think about it: a package is a recipe card the core follows, not an app you install.

This has three consequences worth stating plainly:

- **Writing a package requires minimal effort.** It's YAML and some knowledge of the API you're describing.
- **Installing a package is safe.** A description can't read your environment, open a socket, or touch your filesystem, because it isn't a program. Nobody else's code ever runs on your machine.
- **Every package gets retries, rate limiting, and backoff for free**, because the core does them. A package author can't get retry logic wrong, because they don't write any.

Packages live on GitHub and are referenced by module path and version:

```yaml
uses: "github.com/myorg/datasplice-zendesk@v0.2.0"
```

The core downloads the manifest, verifies it against a lockfile, and caches it.

For the full format, see [manifest-spec.md](./manifest-spec.md).

### Built-in packages

A few packages ship with the core and need no version, since they're pinned to the core's own version:

- `datasplice/csv`: read and write CSV
- `datasplice/json`: read and write JSON
- `datasplice/http`: a generic HTTP source for APIs that don't have a package yet
- `datasplice/map`: select, rename, and coerce fields

### How data moves

A flow is an ordered list of steps. The first one imports, the last one exports, and anything between transforms.

Records flow through the steps in order. The core doesn't load the whole dataset into memory — a source that paginates emits records as each page arrives, and they move downstream from there.

Each step can process records one at a time or in batches. Batching matters when a step calls a paid API: sending 100 tickets in one request costs less than 100 requests. That's configured per step.

### An example flow

Zendesk tickets → an LLM for context → a CSV file:

```
datasplice-zendesk       →   datasplice-anthropic      →   datasplice/csv
(fetch tickets, 100      →   (send each batch to       →   (write rows)
 per page, paginated)        Claude, keep the reply)
```

The core makes every one of those HTTP calls. The two packages only describe what the calls look like.

The full config for this is in [file-structure.md](./file-structure.md).

### What Datasplice is not

- **Not a general workflow engine.** No branching, no loops, no conditionals. A flow is a straight line.
- **Not a transformation language.** Field mapping uses dotted paths. If you need computed columns or joins, this is the wrong tool.
- **Not real-time.** Flows run on demand or on a schedule.
- **Not a hosted service.** It's a binary you run, on your machine or in CI or in a container.

### Current limits

Some APIs can't be described declaratively — request signing (AWS SigV4, Shopify HMAC), GraphQL, and anything needing custom logic to untangle a response.

The plan is a sandboxed code hook (compiled to WASM, so it still can't touch your machine) for these cases. It isn't built yet. The manifest format reserves a `hook:` key so it can be added without breaking existing packages.
