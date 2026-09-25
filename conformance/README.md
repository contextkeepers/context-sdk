# Conformance cases

Three clients, one set of behaviours. Each case is a JSON file describing a
client config, a scripted sequence of calls, and the HTTP requests that must
come out the other side. Every language ships a small runner that reads these
files, drives its own client, and diffs the result.

There is no server here. The runner substitutes a fake transport and a fake
clock, which is what keeps these tests fast and deterministic - `advance_ms`
cannot be done against a real clock without `sleep`, and `sleep` in CI is how
suites become flaky.

## Case format

```json
{
  "name": "flushes_on_batch_size",
  "config":    { "batch_size": 3, "flush_interval_ms": 60000 },
  "responses": [ { "status": 200 } ],
  "steps":     [ { "log": { "event": "a" } } ],
  "expect":    { "requests": [ { "entries": [ { "event": "a" } ] } ] }
}
```

`config` - client constructor options. A case only names what it cares about;
everything else comes from the table below.

| Key | Runner default |
|---|---|
| `tenant_id` | `tenant_test` |
| `batch_size` | 20 |
| `flush_interval_ms` | 5000 |
| `max_queue_size` | 10000 |
| `max_retries` | 3 |
| `close_timeout_ms` | 5000 |

These are the *runner's* defaults, not any client's. The libraries differ on
purpose - a browser batches at 20 where a server batches at 500 - so a runner
passes every value explicitly, or the same case would mean different things in
different languages.

`responses` - consumed in order by the fake transport, one per request. A case
that runs out gets `{"status": 200}` for every request after. A response may carry
`"rejected": n` alongside a 400, standing for a per-entry report of that size. Keeping them here
rather than inline in `steps` means a case never depends on the runner and the
client interleaving in a particular order.

`why` - prose, ignored by runners. A case that fails should explain what
behaviour it was protecting without anyone having to reconstruct it.

`steps` - applied in order, each exactly one of:

| Step | Effect |
|---|---|
| `{"log": {"event": ..., "metadata": ...}}` | One `log()` call on the current scope. |
| `{"identify": {...}}` | Opens a scope from the current one and makes it current. |
| `{"scope": "base"}` | Go back to the client's own scope, as a logout would. |
| `{"advance_ms": 5000}` | Move the fake clock, firing any timers due. |

The fake clock starts at Unix epoch 0, so a case can assert exact timestamps:
after `{"advance_ms": 30000}` the clock reads `1970-01-01T00:00:30.000Z`.

`advance_ms` means *let the whole window happen*, not "fire the timers that are
due right now." A retry schedules the next retry only once its request resolves,
so a runner has to drain in-flight work between firings - otherwise a case about
three attempts would need three `advance_ms` steps, and the fixture would be
encoding the client's internals instead of its behaviour.
| `{"flush": {}}` | Explicit flush, awaited. |
| `{"reset": {}}` | Forget the current identity, as a logout would. |
| `{"close": {}}` | Shut down, draining. |

Two modifiers apply to `log` and `identify`, for cases about more than one
identity at a time:

| Modifier | Effect |
|---|---|
| `"via": "<name>"` | Act on the named scope instead of the current one. |
| `"as": "<name>"` | Store the new scope under this name, leaving the current one alone. |

`expect` - any of:

| Key | Asserts |
|---|---|
| `requests` | The full ordered list of request bodies. |
| `request_count` | How many requests were attempted. |
| `dropped` | The client's reported count of discarded events. |
| `error` | The call raised or returned this error kind. |

## When a case cannot apply

```json
"skip": { "go": "Identify takes a struct, so an unknown field is a compile error" }
```

A statically typed client can make a behaviour unreachable rather than merely
tested - there is no way to write the call that `rejects_unknown_actor_key`
describes against Go's `Identify(Actor)`. A runner skips those and prints the
reason, which is a stronger result than a pass, not a gap. Use it only for that:
a behaviour a client simply has not implemented is a failure.

## Matching

Request bodies match **partially**: only the keys a case names are compared, at
every level of nesting. So a case about batching says `{"event": "a"}` and stays
silent about `timestamp` and `tenant_id`, and adding a field later does not break
every file.

Arrays are exact in length and order - `entries` is the thing most cases are
about, so a missing or extra event has to fail.

`expect.requests` and `expect.request_count` are mutually exclusive: name the
bodies when they matter, the count when only retry behaviour does.
