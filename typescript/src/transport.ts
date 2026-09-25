import type { Batch, Response, Transport } from "./types.ts";

// Long enough to cover a wait_for_async_insert flush, short enough that
// a black-holed connection cannot block close() indefinitely.
export const REQUEST_TIMEOUT_MS = 10000;

export const DEFAULT_ENDPOINT =
  "https://clogging.staging.us-west-2.contextkeepers.com/logs/ingest";

// A refused connection and a 500 are the same thing to a caller that
// only decides whether to retry, so a failure becomes status 0.
export class HttpTransport implements Transport {
  private readonly endpoint: string;

  constructor(endpoint: string) {
    this.endpoint = endpoint;
  }

  async send(batch: Batch): Promise<Response> {
    try {
      const res = await fetch(this.endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(batch),
        keepalive: true,
        signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
      });
      // A 400 may carry a per-entry report, which says the rest of the
      // batch was stored. Anything unreadable falls back to total loss.
      if (res.status === 400) {
        try {
          const body = (await res.json()) as { rejected?: unknown[] };
          if (Array.isArray(body.rejected)) {
            return { status: 400, rejected: body.rejected.length };
          }
        } catch {
          // Not the documented shape; treat the batch as lost.
        }
      }
      return { status: res.status };
    } catch {
      return { status: 0 };
    }
  }

  // The page is going away, so this is the last chance to send and
  // fetch will not outlive it. Unacknowledged by design.
  sendBeacon(batch: Batch): boolean {
    if (typeof navigator === "undefined" || !navigator.sendBeacon) return false;
    return navigator.sendBeacon(
      this.endpoint,
      new Blob([JSON.stringify(batch)], { type: "application/json" }),
    );
  }
}
