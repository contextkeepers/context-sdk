"""A stand-in for clogging, so the examples run with nothing deployed.

Prints each batch it receives, which is the point - the examples are
about what goes on the wire, not about what the server does with it.

    python3 examples/mock_ingest.py
"""

import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = 8099


class Handler(BaseHTTPRequestHandler):
    def do_POST(self) -> None:
        length = int(self.headers.get("Content-Length", 0))
        batch = json.loads(self.rfile.read(length) or b"{}")
        entries = batch.get("entries", [])

        print(f"\n--- {len(entries)} event(s) ---")
        for entry in entries:
            print(json.dumps(entry, indent=2, sort_keys=True))
        sys.stdout.flush()

        body = json.dumps({"ingested": len(entries)}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args) -> None:
        pass


if __name__ == "__main__":
    print(f"mock ingest on http://127.0.0.1:{PORT}/logs/ingest")
    HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
