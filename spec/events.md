# The event wire format

The contract every client implements and clogging accepts. This file is the
source of truth: the TypeScript, Python and Go clients are three renderings of
it, and `conformance/cases` is it made executable.

Storage is not in scope here. How clogging lays these fields out in ClickHouse
is still open (`context-backend/clogging/schema.md`) and can change without
touching this document - which is the point of writing the wire down separately.

---

## Endpoint

```
POST https://clogging.staging.us-west-2.contextkeepers.com/logs/ingest
Content-Type: application/json
```

No authentication. `tenant_id` is asserted by the caller and verified by nothing,
so anything holding it can write as any tenant. A write key resolved server-side
is the planned fix; when it lands the tenant moves to a header and this body shape
does not change.

## The batch envelope

Always a batch, even for one event, so a client that buffers pays one round trip
rather than one per event.

```json
{ "entries": [ { /* event */ }, { /* event */ } ] }
```

At most 2,000 entries per request, and **each entry at most 4,096 bytes**. Past
that a caller is logging documents rather than events.

Entries are judged one at a time. An entry that is too big, malformed, or missing
a required field is dropped on its own; the rest of the batch is stored. The
response names every rejection by index, so a caller still knows exactly what
landed - which is what makes per-entry rejection safe where all-or-nothing used to
be the only honest answer.

One limit on that: a JSON **syntax** error anywhere in the body fails the whole
request, because there is no way to parse an array around a broken token. Only
entries that are valid JSON of the wrong shape can be rejected individually.

## The event

```json
{
  "timestamp": "2026-09-25T14:03:11.482Z",
  "tenant_id": "org_acme",
  "event": "checkout_completed",
  "actor": { "user_id": "user_123", "organization_id": "org_42", "device_id": "camera_7" },
  "metadata": { "plan": "pro", "amount": 49 }
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `tenant_id` | string | yes | Which customer of ours. Set once, on the client. |
| `event` | string | yes | `snake_case`, past tense. Low cardinality - a name, never an id. |
| `timestamp` | RFC 3339, ms, UTC | yes | Client clock, stamped when `log()` was called. |
| `actor` | object | no | Who or what emitted it. Sticky client state. |
| `metadata` | object | no | Per-call properties. Caller's keys, caller's types. |

A client MUST stamp `timestamp` when `log()` is called, not when the batch is
sent, and the server MUST reject an entry without one. An event queued for 30s
and then flushed happened 30s ago; stamping at flush time collapses a whole
session into one instant and makes funnel ordering meaningless.

It is required rather than defaulted because a server-side default is
indistinguishable from a real value and wrong by however long the batch waited -
a silently plausible timestamp is worse than a rejected batch. Every client
stamps it on every event, so nothing legitimate is refused.

### `event` names

The column behind this is `LowCardinality`, and it leads the sort key after
`tenant_id`. Unbounded distinct values degrade every read, so an id belongs in
`actor` or `metadata`, never in the name.

```
checkout_completed        good
rows_processed            good
user_123_checked_out      bad - unbounded
Checkout Completed        bad - not snake_case
```

### `actor`

Provenance, not payload. The client sets it once and attaches it to every
subsequent event; the caller never passes it per event.

| Key | Set by | When |
|---|---|---|
| `user_id` | `identify()` | A person is logged in. |
| `organization_id` | `identify()` | The organisation, team or workspace inside the *customer's* product that the person acted in. |
| `device_id` | `identify()` | The machine or instrument the event came from - a kiosk, a camera, a sensor, a browser install the caller persists itself. |
| `process_id` | `identify()` | A batch job or service, where no person is behind it. |

All are optional and all may be absent together - a service event has no person,
no organisation, no machine and possibly no job id either. They co-occur freely:
a person acting inside an organisation from a particular camera is three of them
on one event, which is why `identify()` takes several at once.

`organization_id` is not `tenant_id`. `tenant_id` is which customer of ours sent
the event; `organization_id` is a grouping inside that customer's own product.
They are one careless glance apart in a query, which is why they do not share a
word.

### `metadata`

Whatever the caller passes, with types preserved - `49` stays a number, so it can
be summed. Nested objects and arrays are accepted and flattened by path.

A client MUST NOT write identity keys here. `actor` exists so that a caller
passing `{"user_id": "..."}` as a property cannot overwrite the real one.

## Responses

```json
{ "ingested": 2 }
```

```json
{ "ingested": 497,
  "rejected": [ { "index": 3, "reason": "entry is 5101 bytes, over the 4096 byte limit" },
                { "index": 11, "reason": "missing event" } ] }
```

| Status | Meaning | Client action |
|---|---|---|
| 200 | Every entry stored and flushed. `rejected` is absent. | Drop the batch. |
| 400 | Some or all entries refused. `rejected` says which. | Never retry. Count **only** the rejected as lost - the rest are stored. |
| 400, no `rejected` | The body itself was unparseable or over 8 MiB. | Never retry. The whole batch is lost. |
| 429 | Rate limited. | Retry with backoff. |
| 5xx | Ours. | Retry with backoff. |
| network error / timeout | Unknown - it may have been stored. | Retry with backoff. |

A 400 is deliberate rather than a 200 with a report: a caller sending entries that
cannot be stored should hear about it loudly, in whatever already watches for 4xx.
The body is what keeps that from costing them the good entries.

A 200 means durable, not accepted: the server waits for its flush before
answering. So it costs real latency, which is what makes client-side batching
load-bearing rather than an optimisation.

Retries are therefore at-least-once and can duplicate. Dedupe is not yet
possible server-side; a client-generated `insert_id` is the planned fix.

---

## What every client must do

Identical across all three languages. `conformance/cases` is what holds them to
it.

**Batch.** Flush on whichever comes first: entry count, or a time interval.
Defaults are 20 events / 5s in a browser and 500 events / 5s on a server - a
frontend trades throughput for not losing a session, a backend the reverse.

**Retry.** Exponential backoff with jitter, on 429, 5xx and network errors only.
Never on any other 4xx. Bounded attempts, then drop and count.

**Bound the queue.** A fixed maximum of pending events. On overflow drop the
oldest and expose a count of what was dropped. Never grow without limit, and
never block the caller.

**Never enqueue an entry the server cannot store.** A client MUST serialise each
event at `log()` and drop it there - counted - if it exceeds 4,096 bytes or cannot
be encoded at all. One 5 KB event must not cost the 499 good ones batched beside
it, and the caller finds out at the call site rather than from a rejection report
minutes later. This also makes the batch size bounded by construction: 2,000
entries of 4 KB fits inside the body limit with no separate byte budget.

**Never throw into the caller's path.** `log()` returns immediately and cannot
raise - not on a full queue, not on a dead network, not on unserialisable
properties. A logging client that breaks the page or fails the job is worse than
one that loses an event.

`identify()` is the exception and MUST fail loudly on an unreserved key - raising
where that is the idiom, returning an error where it is not. That is a typo in
the integration rather than bad runtime data, it is discovered on the first call,
and failing quietly would leave identity permanently unqueryable.

**Flush on the way out.** A browser client flushes on `visibilitychange` and
`pagehide` via `sendBeacon`, or it loses the last event of every session. A
server client drains on `close()`.

`close()` MUST be reached from the process's own shutdown handling, not from a
deferred call or an exit hook. Containers are stopped with SIGTERM, and Go's
default action on SIGTERM terminates the process without running deferred
functions at all - so `defer client.Close()` in a long-lived service loses the
buffer on every deploy. SIGKILL runs nothing, anywhere, by design.

`close()` blocks, and that is the feature - a 200 means the events are stored, so
waiting is what makes the guarantee worth anything. But the blocking MUST be
bounded by a configured deadline (`closeTimeoutMs` / `close_timeout_ms` /
`CloseTimeout`, default 5s), set comfortably shorter than the container's grace
period. An unbounded drain is self-defeating: outlive the grace period and SIGKILL
lands mid-send, losing everything it was holding rather than the part it could
not send.

Within that deadline a client MUST retry, since a rolling restart of the sink is
exactly when the last batch fails once. At the deadline it MUST drop what remains,
count it, and **report the number** - a deploy quietly losing a batch per instance
is the kind of thing nobody notices for months.

What that leaves unprotected is bounded by the flush interval: a crash or a
SIGKILL can lose at most one interval's worth of events. That interval is the
knob if either is a real risk, traded against round trips.

A client MAY offer an opt-in signal helper that drains and then restores the
default action and re-raises, so the exit status the supervisor sees is unchanged.
It MUST NOT install one by default: registering a signal suppresses Go's default
termination, so a library doing it unasked stops the caller's process from exiting
on SIGTERM at all unless they also handle it - turning a graceful stop into a
grace-period hang followed by SIGKILL.

A client MUST also survive a retry that was armed before `close()` and fires
after it. Dropping and counting that batch is correct; reaching into a shut down
dispatcher is not - it crashes the caller's process, which is the one thing a
logging client must never do.

**Take an injectable clock and transport.** Not for customers - for the
conformance suite, which cannot test interval flushing or backoff against a real
clock without becoming slow and flaky.

## Versioning

Additive changes - a new optional field, a new reserved `actor` key - are not
breaking, and clients MUST ignore response fields they do not recognise.
Anything else gets a new path, because customer bundles ship on their schedule
and old clients keep writing for a long time.
