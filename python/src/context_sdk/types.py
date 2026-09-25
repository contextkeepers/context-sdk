"""The wire format, as spec/events.md defines it."""

import datetime
from typing import Any, Callable, Dict, List, NamedTuple, Optional, Protocol

# The only keys actor accepts, exported for callers that need the list.
# identify() takes them as named parameters, so a typo is a TypeError
# rather than something this has to catch. organization_id is the
# customer's own organisation, not our tenant - that is tenant_id.
RESERVED_ACTOR_KEYS = ("user_id", "device_id", "process_id", "organization_id")

# One event's budget on the wire. Past this the server refuses the entry,
# so the client drops it at log() instead of batching it.
MAX_ENTRY_BYTES = 4096


class Response(NamedTuple):
    """status 0 means the request never got an answer. rejected is how
    many entries the server refused, present only on a 400 that carried
    a report - the rest of that batch was stored."""

    status: int
    rejected: Optional[int] = None


def iso_ms(millis: int) -> str:
    """RFC 3339 with exactly three decimals, which is what the column stores."""
    whole = datetime.datetime.fromtimestamp(millis // 1000, tz=datetime.timezone.utc)
    return whole.strftime("%Y-%m-%dT%H:%M:%S.") + f"{millis % 1000:03d}Z"


class Clock(Protocol):
    def now(self) -> int: ...
    def set_timer(self, delay_ms: float, fn: Callable[[], None]) -> Any: ...
    def clear_timer(self, handle: Any) -> None: ...

    def sleep(self, delay_ms: float) -> None:
        """Blocking wait, so close() can back off inside its deadline.
        Real time in production, instant under the conformance cases."""
        ...


class Transport(Protocol):
    def send(self, batch: Dict[str, List[Dict[str, Any]]]) -> "Response": ...


class Dispatcher(Protocol):
    """Where a background send runs. log() must never block on one."""

    def submit(self, fn: Callable[[], None]) -> None: ...
    def drain(self) -> None: ...
    def shutdown(self) -> None: ...
