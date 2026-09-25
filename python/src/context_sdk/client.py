import json
import random
import threading
from typing import Any, Dict, Optional

from .clock import SystemClock
from .dispatch import ThreadDispatcher
from .queue import EventQueue
from .transport import DEFAULT_ENDPOINT, HttpTransport
from .types import MAX_ENTRY_BYTES, Clock, Dispatcher, Transport, iso_ms

# Server defaults: a backend trades a longer window for fewer round
# trips, where a browser would rather not lose a session.
DEFAULT_BATCH_SIZE = 500
DEFAULT_FLUSH_INTERVAL_MS = 5000
DEFAULT_MAX_QUEUE_SIZE = 10000
DEFAULT_MAX_RETRIES = 3
# Inside ECS's 30s default grace period, with room for the app's own
# shutdown work.

RETRY_BASE_MS = 500
RETRY_CAP_MS = 30000


def _retryable(status: int) -> bool:
    """429 and 5xx are worth another attempt; 0 is a request that got no
    answer. Every other status will fail identically forever."""
    return status == 0 or status == 429 or status >= 500


def _actor(
    user_id: Optional[str],
    device_id: Optional[str],
    process_id: Optional[str],
    organization_id: Optional[str],
) -> Dict[str, str]:
    """Only the names below exist, because they are parameters rather
    than dict keys - a typo is a TypeError at the call site."""
    pairs = (
        ("user_id", user_id),
        ("device_id", device_id),
        ("process_id", process_id),
        ("organization_id", organization_id),
    )
    return {key: value for key, value in pairs if value is not None}


class Logger:
    """One identity's view of a client.

    Immutable: narrowing returns a new Logger, so a request-scoped
    logger cannot disturb the one it came from, and two threads cannot
    overwrite each other's identity the way shared client state would.
    """

    def __init__(self, context: "Context", actor: Dict[str, str]) -> None:
        self._context = context
        self._actor = dict(actor)

    def log(self, event: str, metadata: Optional[Dict[str, Any]] = None) -> None:
        self._context._enqueue(self._actor, event, metadata or {})

    def identify(
        self,
        *,
        user_id: Optional[str] = None,
        device_id: Optional[str] = None,
        process_id: Optional[str] = None,
        organization_id: Optional[str] = None,
    ) -> "Logger":
        """Adds to this identity without touching it."""
        merged = dict(self._actor)
        merged.update(_actor(user_id, device_id, process_id, organization_id))
        return Logger(self._context, merged)


class Context:
    def __init__(
        self,
        tenant_id: str,
        endpoint: str = DEFAULT_ENDPOINT,
        batch_size: int = DEFAULT_BATCH_SIZE,
        flush_interval_ms: int = DEFAULT_FLUSH_INTERVAL_MS,
        max_queue_size: int = DEFAULT_MAX_QUEUE_SIZE,
        max_retries: int = DEFAULT_MAX_RETRIES,
        clock: Optional[Clock] = None,
        transport: Optional[Transport] = None,
        dispatcher: Optional[Dispatcher] = None,
    ) -> None:
        self._tenant_id = tenant_id
        self._batch_size = batch_size
        self._flush_interval_ms = flush_interval_ms
        self._max_retries = max_retries
        self._clock = clock or SystemClock()
        self._transport = transport or HttpTransport(endpoint)
        self._dispatcher = dispatcher or ThreadDispatcher()
        self._queue = EventQueue(max_queue_size)

        self._closed = False
        self._timer: Any = None
        self._lock = threading.Lock()

    def identify(
        self,
        *,
        user_id: Optional[str] = None,
        device_id: Optional[str] = None,
        process_id: Optional[str] = None,
        organization_id: Optional[str] = None,
    ) -> Logger:
        """Opens a scope for whoever the following events belong to.

        Returns a logger rather than setting client state, so two
        threads cannot attribute each other's events.
        """
        return Logger(self, _actor(user_id, device_id, process_id, organization_id))

    def log(self, event: str, metadata: Optional[Dict[str, Any]] = None) -> None:
        """For events with nobody behind them - a cache warming, a job
        the process itself did."""
        self._enqueue({}, event, metadata or {})

    def flush(self) -> None:
        """Sends everything pending, retries included."""
        self._disarm()
        with self._lock:
            batch = self._queue.drain()
        if batch:
            self._send(batch, 0)

    def close(self) -> None:
        """Drains before returning - blocking is the point, since a 200
        means the events are stored.

        One attempt per batch, no retry: this is the caller's shutdown
        budget, and a second attempt spends time they did not offer.
        Whatever the attempt does not store is counted in dropped.
        """
        with self._lock:
            self._closed = True
        self._disarm()

        # Work already handed to a background send, which the loop below
        # does not own.
        self._dispatcher.drain()

        with self._lock:
            batch = self._queue.drain()

        while batch:
            status, rejected = self._transport.send({"entries": batch})
            if 200 <= status < 300:
                pass
            elif status == 400 and rejected is not None:
                # The server named what it refused; the rest was stored.
                self._queue.record_dropped(rejected)
            else:
                self._queue.record_dropped(len(batch))
            # Whatever arrived while that send was in flight, sent on the
            # same one-attempt terms.
            with self._lock:
                batch = self._queue.drain()

        self._dispatcher.shutdown()

    @property
    def dropped(self) -> int:
        return self._queue.dropped

    def __enter__(self) -> "Context":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    def _enqueue(self, actor: Dict[str, str], event: str, metadata: Dict[str, Any]) -> None:
        """The seam a Logger writes through. Never raises - a logging
        client that breaks the caller is worse than one that loses an
        event."""
        with self._lock:
            if self._closed:
                self._queue.record_dropped(1)
                return
            # Stamped here rather than at flush: the event happened now,
            # not whenever the batch happens to drain.
            entry = {
                "timestamp": iso_ms(self._clock.now()),
                "tenant_id": self._tenant_id,
                "event": event,
                "actor": dict(actor),
                "metadata": metadata,
            }
            # An entry the server will refuse must never join a batch, or
            # it costs every good event beside it. Encoding here also
            # catches metadata that cannot be serialised at all.
            try:
                encoded = json.dumps(entry).encode("utf-8")
            except (TypeError, ValueError):
                self._queue.record_dropped(1)
                return
            if len(encoded) > MAX_ENTRY_BYTES:
                self._queue.record_dropped(1)
                return

            self._queue.push(entry)
            full = self._queue.size >= self._batch_size

        if full:
            self._dispatcher.submit(self.flush)
        else:
            self._arm()

    def _send(self, batch, attempt: int) -> None:
        status, rejected = self._transport.send({"entries": batch})
        if 200 <= status < 300:
            return

        # A reported rejection means the rest of the batch was stored,
        # so only the named entries are lost.
        if status == 400 and rejected is not None:
            self._queue.record_dropped(rejected)
            return

        if _retryable(status) and attempt < self._max_retries:
            delay = min(RETRY_BASE_MS * (2**attempt), RETRY_CAP_MS)
            # Jitter keeps a fleet of clients from retrying in lockstep.
            wait = delay / 2 + random.random() * (delay / 2)
            self._clock.set_timer(wait, lambda: self._retry(batch, attempt + 1))
            return
        self._queue.record_dropped(len(batch))

    def _retry(self, batch, attempt: int) -> None:
        """A timer armed before close() can always fire after it. The
        batch is lost then, and counting it beats handing work to a
        dispatcher that has stopped."""
        with self._lock:
            if self._closed:
                self._queue.record_dropped(len(batch))
                return
        self._dispatcher.submit(lambda: self._send(batch, attempt))

    def _arm(self) -> None:
        with self._lock:
            if self._timer is not None:
                return
            self._timer = self._clock.set_timer(self._flush_interval_ms, self._on_timer)

    def _on_timer(self) -> None:
        with self._lock:
            self._timer = None
        self._dispatcher.submit(self.flush)

    def _disarm(self) -> None:
        with self._lock:
            timer, self._timer = self._timer, None
        if timer is not None:
            self._clock.clear_timer(timer)
