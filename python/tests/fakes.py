import copy
import pathlib
import sys
from typing import Any, Callable, Dict, List

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "src"))
from context_sdk.types import Response


class FakeClock:
    """Starts at Unix epoch 0 so a case can assert exact timestamps."""

    def __init__(self) -> None:
        self._now = 0
        self._timers: List[Dict[str, Any]] = []
        self._next_id = 1

    def now(self) -> int:
        return self._now

    def set_timer(self, delay_ms: float, fn: Callable[[], None]) -> Any:
        handle = self._next_id
        self._next_id += 1
        self._timers.append({"id": handle, "at": self._now + delay_ms, "fn": fn})
        return handle

    def clear_timer(self, handle: Any) -> None:
        self._timers = [t for t in self._timers if t["id"] != handle]

    def sleep(self, delay_ms: float) -> None:
        """A blocking wait is just time passing, so cases never sleep."""
        self.advance(delay_ms)

    def advance(self, ms: float) -> None:
        """Lets the whole window happen: a timer fired here may schedule
        another one inside it, so the due set is rescanned each pass."""
        target = self._now + ms
        while True:
            due = sorted([t for t in self._timers if t["at"] <= target], key=lambda t: t["at"])
            if not due:
                break
            first = due[0]
            self._timers = [t for t in self._timers if t["id"] != first["id"]]
            self._now = first["at"]
            first["fn"]()
        self._now = target


class FakeTransport:
    def __init__(self, responses: List[Dict[str, Any]] = None) -> None:
        self.sent: List[Dict[str, Any]] = []
        self._queued = list(responses or [])

    def send(self, batch) -> Response:
        self.sent.append(copy.deepcopy(batch))
        if self._queued:
            queued = self._queued.pop(0)
            return Response(queued["status"], queued.get("rejected"))
        return Response(200)
