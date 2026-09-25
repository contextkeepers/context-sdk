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
| `python/` | The same, via the context manager, which closes on the way out. |
| `go/` | The same, with `defer client.Close()`. |

The frontend concerns with no server-side equivalent - dropping a scope at
logout, and flushing on the way out so the last event of a session survives - are
in `spec/events.md` rather than in an example, since a browser cannot run
TypeScript without a bundler and an example nobody can execute earns its keep
less than the spec does.

## These are jobs, not services

Both examples exit normally, so `close()` on the way out is enough. A long-lived
service is different: SIGTERM terminates a Go process without running deferred
functions, so `close()` has to hang off the same signal handling the rest of the
service already uses.

```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
<-quit

httpServer.Shutdown(ctx)
if err := client.Close(); err != nil {
    log.Printf("logging shutdown: %v", err)   // names what it could not send
}
```

For a process whose *only* shutdown work is the client - a worker, a CLI, a cron
job - `ctxsdk.CloseOnSignal(client)` does the same thing in one line, drains, then
restores the default action and re-raises so the exit status is unchanged. Don't
use it in a service that also drains connections: whichever handler finishes first
wins, and if it is the client's you lose the shutdown events you were saving.

## Two things the examples are actually about

**Draining on exit.** Every client buffers, so a short-lived process that exits
without `close()` silently drops whatever had not reached the batch threshold -
usually the events describing how the job finished.

**Scopes, not client state.** `identify()` returns a logger and never touches the
client, so the per-record scope cannot disturb the job's and two of them running
concurrently cannot attribute each other's events. That is why the loop narrows
`job` rather than re-identifying the client.
