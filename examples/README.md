# Examples

One per client, all three runnable. Start the stand-in server first, in its own
terminal - it prints every batch it receives, which is the part worth looking at.

```
python3 examples/mock_ingest.py
```

Then, from the repo root:

```
node examples/node/main.ts        # needs node 23+, no install
python3 examples/python/main.py
cd examples/go && go run .
```

Each does the same thing - a batch job opening a scope for itself, narrowing it
per record to the organisation and person involved, and draining on the way out -
so the three read as translations of one another.

| Example | Shows |
|---|---|
| `node/` | A backend service in TypeScript. `close()` drains what the batch threshold did not. |
| `python/` | The same, calling `close()` outright - see below for why not `with`. |
| `go/` | The same, with `defer client.Close()`, which a signal-driven return also reaches. |

The frontend concerns with no server-side equivalent - dropping a scope at
logout, and flushing on the way out so the last event of a session survives - are
in `spec/events.md` rather than in an example, since a browser cannot run
TypeScript without a bundler and an example nobody can execute earns its keep
less than the spec does.

## A job and a service close differently

These examples are jobs. A job exits normally, so closing on the way out is
enough, which is why none of them handles a signal.

Go reaches `defer client.Close()` on any normal return, so the example uses it.
Python's example calls `close()` outright instead: Python installs no default
SIGTERM handler, so a container stopped by one skips `__exit__`, `finally` and
`atexit` alike. SIGINT *does* raise `KeyboardInterrupt` and unwind through all
three, so `with` looks correct under ctrl-c and loses the buffer on every deploy.
Reaching for `with` in a service means also installing
`signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))`.

A long-lived service never exits on its own, so it has no "way out" to close on -
the close hangs off the signal handling the service already has. There is one
ordering, and it is the whole of it:

```go
<-ctx.Done()                      // signal

server.Shutdown(shutdownCtx)      // 1. stop serving: no new events
client.Log("service_stopped", …)  // 2. log the shutdown itself
client.Close()                    // 3. drain last, one attempt
log.Printf("dropped=%d", client.Dropped())
```

Serving stops first, so nothing can log into a queue that is already draining.
The shutdown event is logged before the drain, because a drain only sends what is
already queued. `Close` is last, and once.

The SDK deliberately offers no helper that hangs this off the signal for you. One
that drains and re-raises has to decide when the process dies, and it cannot know
what else your shutdown still has to do - so it either cuts your shutdown short or
drops the events your own handler was about to log. Those four lines belong to the
caller.

## Two things the examples are actually about

**Draining on exit.** Every client buffers, so a short-lived process that exits
without `close()` silently drops whatever had not reached the batch threshold -
usually the events describing how the job finished.

**Scopes, not client state.** `identify()` returns a logger and never touches the
client, so the per-record scope cannot disturb the job's and two of them running
concurrently cannot attribute each other's events. That is why the loop narrows
`job` rather than re-identifying the client.
