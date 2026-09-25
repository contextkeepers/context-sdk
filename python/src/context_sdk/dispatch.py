import queue
import threading
from typing import Callable, Optional

_STOP = object()


class ThreadDispatcher:
    """One worker thread, so log() hands off instead of waiting on HTTP."""

    def __init__(self) -> None:
        self._work: "queue.Queue" = queue.Queue()
        self._idle = threading.Event()
        self._idle.set()
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def submit(self, fn: Callable[[], None]) -> None:
        self._idle.clear()
        self._work.put(fn)

    def drain(self) -> None:
        self._work.join()

    def shutdown(self) -> None:
        self._work.put(_STOP)
        self._thread.join(timeout=5)

    def _run(self) -> None:
        while True:
            item = self._work.get()
            if item is _STOP:
                self._work.task_done()
                return
            try:
                item()
            except Exception:
                # A logging client must not take the process down.
                pass
            finally:
                self._work.task_done()


class InlineDispatcher:
    """Runs the work where it was submitted - what the cases use."""

    def submit(self, fn: Callable[[], None]) -> None:
        fn()

    def drain(self) -> None:
        pass

    def shutdown(self) -> None:
        pass
