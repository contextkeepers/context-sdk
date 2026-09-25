# context-sdk

Clients for sending events to Context, in TypeScript, Python and Go.

```ts
import { Context } from "@context/sdk";

const ctx = new Context({ tenantId: "org_acme" });

const actor = ctx.identify({ user_id: "user_123", organization_id: "org_42" });
actor.log("checkout_completed", { plan: "pro", amount: 49 });
```

See `examples/` to run one against a local stand-in server.

## Layout

| Path | What |
|---|---|
| `spec/events.md` | The wire format. Source of truth for all three clients. |
| `conformance/` | Shared behaviour cases every client's tests run. |
| `typescript/` | `@context/sdk` - browser and node. |
| `python/` | `context-sdk` on PyPI. |
| `go/` | `github.com/contextkeepers/context-sdk/go`, package `ctxsdk`. |
| `examples/` | One runnable example per client, against a local stand-in server. |

Three clients drift unless something holds them together, so the order matters:
the spec is written first, the cases make it executable, and each client is then
judged against both rather than against its own tests.

## Running the cases

```
just test                      # all three

cd typescript && npm test      # node 23+; npm run typecheck for the type tests
cd python     && python3 -m unittest discover -s tests
cd go         && go test ./...
```

`typescript/test/types.ts` is a type-level test: every `@ts-expect-error` in it
must be a real error, so `tsc` fails if a typo'd actor key ever starts
compiling.

## Status

`spec/`, `conformance/` and all three clients are written, each passing the
full case set.
