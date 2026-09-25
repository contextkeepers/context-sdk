// The wire format, as spec/events.md defines it.

export interface Event {
  timestamp: string;
  tenant_id: string;
  event: string;
  actor: Record<string, string>;
  metadata: Record<string, unknown>;
}

export interface Batch {
  entries: Event[];
}

// The only keys actor accepts. A typo here would become another kind of
// identity that no query finds, so it is rejected rather than sent.
// organization_id is the customer's own organisation, not our tenant -
// that is tenant_id, and it is set once on the client.
// One event's budget on the wire. Past this the server refuses the
// entry, so the client drops it at log() instead of batching it.
export const MAX_ENTRY_BYTES = 4096;

export const RESERVED_ACTOR_KEYS = [
  "user_id",
  "device_id",
  "process_id",
  "organization_id",
] as const;

export type ActorKey = (typeof RESERVED_ACTOR_KEYS)[number];
export type Actor = Partial<Record<ActorKey, string>>;

export class UnknownActorKeyError extends Error {
  readonly kind = "unknown_actor_key";
  constructor(key: string) {
    super(
      `"${key}" is not an actor key - use one of ${RESERVED_ACTOR_KEYS.join(", ")}, ` +
        `or pass it as event metadata instead`,
    );
  }
}

export interface Options {
  tenantId: string;
  endpoint?: string;
  // Browser defaults: small batches and a short window, because a lost
  // session costs more than the extra round trips.
  batchSize?: number;
  flushIntervalMs?: number;
  maxQueueSize?: number;
  maxRetries?: number;
  // How long close() may spend draining. Must be comfortably shorter
  // than the container's grace period: a drain that outlives it is
  // SIGKILLed mid-send and loses everything it was holding.
  closeTimeoutMs?: number;
  clock?: Clock;
  transport?: Transport;
}

export interface Clock {
  now(): number;
  setTimer(delayMs: number, fn: () => void): unknown;
  clearTimer(handle: unknown): void;
  // Blocking wait, so close() can back off inside its deadline. Real
  // time in production, instant under the conformance cases.
  sleep(delayMs: number): Promise<void>;
}

// A status of 0 means the request never got an answer. `rejected` is the
// number of entries the server refused out of the batch - present only
// on a 400 that carried a report, where the rest were stored.
export interface Response {
  status: number;
  rejected?: number;
}

export interface Transport {
  send(batch: Batch): Promise<Response>;
}
