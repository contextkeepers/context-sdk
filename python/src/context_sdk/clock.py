import threading
import time
from typing import Any, Callable


class SystemClock:
    def now(self) -> int:
        return int(time.time() * 1000)

    def set_timer(self, delay_ms: float, fn: Callable[[], None]) -> Any:
        timer = threading.Timer(delay_ms / 1000.0, fn)
        timer.daemon = True
        timer.start()
        return timer

    def clear_timer(self, handle: Any) -> None:
        if handle is not None:
            handle.cancel()

    def sleep(self, delay_ms: float) -> None:
        time.sleep(delay_ms / 1000.0)
