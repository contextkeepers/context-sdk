import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { Context, Logger } from "../src/index.ts";
import { FakeClock, FakeTransport } from "./fakes.ts";
import { mismatch } from "./match.ts";

const CASES = join(dirname(fileURLToPath(import.meta.url)), "../../conformance/cases");

interface Case {
  name: string;
  why?: string;
  config?: Record<string, number | string>;
  responses?: { status: number }[];
  steps: Record<string, any>[];
  skip?: Record<string, string>;
  expect: {
    requests?: unknown[];
    request_count?: number;
    dropped?: number;
    error?: string;
  };
}

// Shared defaults, so a case only names what it is about.
function build(kase: Case) {
  const config = kase.config ?? {};
  const clock = new FakeClock();
  const transport = new FakeTransport(kase.responses ?? []);
  const client = new Context({
    tenantId: (config.tenant_id as string) ?? "tenant_test",
    batchSize: (config.batch_size as number) ?? 20,
    flushIntervalMs: (config.flush_interval_ms as number) ?? 5000,
    maxQueueSize: (config.max_queue_size as number) ?? 10000,
    maxRetries: (config.max_retries as number) ?? 3,
    clock,
    transport,
  });
  return { client, clock, transport };
}

for (const file of readdirSync(CASES).filter((f: string) => f.endsWith(".json")).sort()) {
  const kase: Case = JSON.parse(readFileSync(join(CASES, file), "utf8"));

  test(kase.name, { skip: kase.skip?.typescript }, async () => {
    const { client, clock, transport } = build(kase);
    let raised: string | null = null;

    // `current` is the scope an unqualified step uses; `via` picks a
    // named one and `as` stores one without changing `current`.
    let current: Context | Logger = client;
    const scopes = new Map<string, Logger>();
    const scopeOf = (step: Record<string, any>): Context | Logger =>
      step.via ? scopes.get(step.via)! : current;

    for (const step of kase.steps) {
      try {
        if ("log" in step) {
          scopeOf(step).log(step.log.event, step.log.metadata ?? {});
        } else if ("identify" in step) {
          const made = scopeOf(step).identify(step.identify);
          if (step.as) scopes.set(step.as, made);
          else current = made;
        } else if ("scope" in step) {
          current = client;
        } else if ("advance_ms" in step) {
          await clock.advance(step.advance_ms, () => client.settled());
        } else if ("flush" in step) {
          await client.flush();
        } else if ("close" in step) {
          await client.close();
        } else {
          assert.fail(`unknown step: ${JSON.stringify(step)}`);
        }
      } catch (err: any) {
        raised = err.kind ?? err.name;
      }
      // Sends are fire-and-forget, so let them land before the next step.
      await client.settled();
    }

    const want = kase.expect;
    if (want.error !== undefined) {
      assert.equal(raised, want.error, `${kase.name}: ${kase.why ?? ""}`);
    } else {
      assert.equal(raised, null, `unexpected error: ${raised}`);
    }
    if (want.requests !== undefined) {
      const found = mismatch(transport.sent, want.requests, "requests");
      assert.equal(found, null, `${found}\n  why: ${kase.why ?? ""}`);
    }
    if (want.request_count !== undefined) {
      assert.equal(transport.sent.length, want.request_count, kase.why ?? "");
    }
    if (want.dropped !== undefined) {
      assert.equal(client.dropped, want.dropped, kase.why ?? "");
    }
  });
}
