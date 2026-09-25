// A backend service emitting events. Run the mock first:
//
//   python3 examples/mock_ingest.py
//   node examples/node/main.ts

import { Context } from "../../typescript/src/index.ts";

const ctx = new Context({
  tenantId: "org_acme",
  endpoint: "http://127.0.0.1:8099/logs/ingest",
  batchSize: 3,
});

// The job itself is the actor for anything not tied to a customer.
const job = ctx.identify({ process_id: "nightly_rollup_2026_09_25" });

for (const [org, user] of [["org_42", "user_1"], ["org_42", "user_2"], ["org_77", "user_3"]]) {
  // A scope per record, narrowed from the job's. The job scope is
  // untouched, so concurrent work cannot cross-attribute.
  const actor = job.identify({ organization_id: org, user_id: user });
  actor.log("rows_rolled_up", { rows: 4096, duration_ms: 120 });
}

// Sends whatever the batch threshold did not. A job that exits without
// this loses its last events; the count says what it could not send.
const abandoned = await ctx.close();
console.log(abandoned === 0 ? "done" : `done, ${abandoned} event(s) unsent`);
