import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "src"))

from context_sdk import Context  # noqa: E402
from context_sdk.dispatch import InlineDispatcher  # noqa: E402
from fakes import FakeClock, FakeTransport  # noqa: E402
from match import mismatch  # noqa: E402

CASES = pathlib.Path(__file__).resolve().parents[2] / "conformance" / "cases"

# The runner's defaults, not the library's - the two differ on purpose
# (a browser batches smaller than a server), and a case must mean the
# same thing in every language.
RUNNER_DEFAULTS = {
    "tenant_id": "tenant_test",
    "batch_size": 20,
    "flush_interval_ms": 5000,
    "max_queue_size": 10000,
    "max_retries": 3,
    "close_timeout_ms": 5000,
}


class ConformanceTest(unittest.TestCase):
    def test_cases(self) -> None:
        files = sorted(CASES.glob("*.json"))
        self.assertTrue(files, f"no cases found in {CASES}")

        for path in files:
            case = json.loads(path.read_text())
            skip = (case.get("skip") or {}).get("python")
            if skip:
                continue
            with self.subTest(case=case["name"]):
                self._run(case)

    def _run(self, case) -> None:
        config = {**RUNNER_DEFAULTS, **(case.get("config") or {})}
        clock = FakeClock()
        transport = FakeTransport(case.get("responses"))
        client = Context(
            clock=clock,
            transport=transport,
            dispatcher=InlineDispatcher(),
            **config,
        )

        # `current` is the scope an unqualified step uses; `via` picks a
        # named one and `as` stores one without changing `current`.
        current = client
        scopes = {}

        def scope_of(step):
            return scopes[step["via"]] if "via" in step else current

        raised = None
        for step in case["steps"]:
            try:
                if "log" in step:
                    scope_of(step).log(step["log"]["event"], step["log"].get("metadata") or {})
                elif "identify" in step:
                    made = scope_of(step).identify(**step["identify"])
                    if "as" in step:
                        scopes[step["as"]] = made
                    else:
                        current = made
                elif "scope" in step:
                    current = client
                elif "advance_ms" in step:
                    clock.advance(step["advance_ms"])
                elif "flush" in step:
                    client.flush()
                elif "close" in step:
                    client.close()
                else:
                    self.fail(f"unknown step: {step}")
            except TypeError as err:
                # identify() takes its keys as named parameters, so the
                # interpreter rejects a typo before our code runs.
                if "unexpected keyword argument" in str(err):
                    raised = "unknown_actor_key"
                else:
                    raised = type(err).__name__
            except Exception as err:
                raised = getattr(err, "kind", type(err).__name__)

        why = case.get("why", "")
        expect = case["expect"]

        if "error" in expect:
            self.assertEqual(raised, expect["error"], why)
        else:
            self.assertIsNone(raised, f"unexpected error: {raised}")
        if "requests" in expect:
            found = mismatch(transport.sent, expect["requests"], "requests")
            self.assertIsNone(found, f"{found}\n  why: {why}")
        if "request_count" in expect:
            self.assertEqual(len(transport.sent), expect["request_count"], why)
        if "dropped" in expect:
            self.assertEqual(client.dropped, expect["dropped"], why)


if __name__ == "__main__":
    unittest.main()
