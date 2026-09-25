"""A backend service emitting events. Run the mock first:

    python3 examples/mock_ingest.py
    python3 examples/python/main.py
"""

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "python" / "src"))

from context_sdk import Context

# The context manager closes on the way out, which is what drains a
# batch that never reached its threshold.
with Context(
    tenant_id="org_acme",
    endpoint="http://127.0.0.1:8099/logs/ingest",
    batch_size=3,
) as ctx:
    # The job itself is the actor for anything not tied to a customer.
    job = ctx.identify(process_id="nightly_rollup_2026_09_25")

    for org, user in (("org_42", "user_1"), ("org_42", "user_2"), ("org_77", "user_3")):
        # A scope per record, narrowed from the job's. The job scope is
        # untouched, so concurrent work cannot cross-attribute.
        actor = job.identify(organization_id=org, user_id=user)
        actor.log("rows_rolled_up", {"rows": 4096, "duration_ms": 120})

print("done")
