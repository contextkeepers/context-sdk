from typing import Any, Dict, List


class EventQueue:
    """Bounded on purpose: an unreachable sink must cost a fixed amount
    of memory, and recent events are worth more than old ones."""

    def __init__(self, max_size: int) -> None:
        self._events: List[Dict[str, Any]] = []
        self._dropped = 0
        self._max_size = max_size

    def push(self, event: Dict[str, Any]) -> None:
        self._events.append(event)
        while len(self._events) > self._max_size:
            self._events.pop(0)
            self._dropped += 1

    def drain(self) -> List[Dict[str, Any]]:
        taken, self._events = self._events, []
        return taken

    @property
    def size(self) -> int:
        return len(self._events)

    @property
    def dropped(self) -> int:
        return self._dropped

    def record_dropped(self, n: int) -> None:
        self._dropped += n
