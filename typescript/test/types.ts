// Type-level assertions. This file passing `tsc --noEmit` is the test:
// every @ts-expect-error below must be an actual error, or tsc fails on
// the unused directive.
import { Context, type Logger } from "../src/index.ts";

const ctx = new Context({ tenantId: "org_acme" });

const member: Logger = ctx.identify({ user_id: "user_123", organization_id: "org_42" });
member.log("checkout_completed", { plan: "pro", amount: 49 });

// Narrowing a scope returns another scope, not void.
const narrower: Logger = member.identify({ device_id: "dev_9f2", session_id: "sess_1" });
narrower.log("clicked");

// @ts-expect-error a typo'd identity key must not compile
ctx.identify({ usr_id: "user_123" });

// @ts-expect-error nor an invented one, however plausible
ctx.identify({ request_id: "req_1" });

// @ts-expect-error actor values are ids, not arbitrary payloads
ctx.identify({ user_id: 123 });

// @ts-expect-error the tenant is not an actor - it is set once, on the client
ctx.identify({ tenant_id: "org_acme" });

// @ts-expect-error identify() no longer mutates, so there is nothing to reset
ctx.reset();

// Metadata is the open map, so anything goes there.
ctx.log("cache_warmed", { entries: 4096, nested: { ok: true } });
