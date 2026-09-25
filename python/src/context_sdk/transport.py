import json
import urllib.error
import urllib.request
from typing import Any, Dict, List

from .types import Response

DEFAULT_ENDPOINT = "https://clogging.staging.us-west-2.contextkeepers.com/logs/ingest"

# Long enough to cover a wait_for_async_insert flush, short enough that
# a hung sink does not pin a worker thread for a minute.
TIMEOUT_SECONDS = 10


class HttpTransport:
    """A refused connection and a 500 are the same to a caller that only
    decides whether to retry, so a failure becomes status 0."""

    def __init__(self, endpoint: str = DEFAULT_ENDPOINT) -> None:
        self._endpoint = endpoint

    def send(self, batch: Dict[str, List[Dict[str, Any]]]) -> Response:
        body = json.dumps(batch).encode("utf-8")
        request = urllib.request.Request(
            self._endpoint,
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=TIMEOUT_SECONDS) as response:
                return Response(response.status)
        except urllib.error.HTTPError as err:
            # A 400 may carry a per-entry report, which says the rest of
            # the batch was stored. Anything unreadable is total loss.
            if err.code == 400:
                try:
                    payload = json.loads(err.read())
                    if isinstance(payload.get("rejected"), list):
                        return Response(400, len(payload["rejected"]))
                except Exception:
                    pass
            return Response(err.code)
        except Exception:
            return Response(0)
