import { systemClock } from "./clock.ts";
import { EventQueue } from "./queue.ts";
import { DEFAULT_ENDPOINT, HttpTransport } from "./transport.ts";
import {
  MAX_ENTRY_BYTES,
  RESERVED_ACTOR_KEYS,
  UnknownActorKeyError,
  type Actor,
  type Clock,
  type Event,
  type Options,
  type Transport,
} from "./types.ts";

const DEFAULTS = {
  batchSize: 20,
  flushIntervalMs: 5000,
  maxQueueSize: 10000,
  maxRetries: 3,
  // Inside ECS's 30s default grace period with room for the app's own
  // shutdown work.
};

// First retry waits this long, doubling each attempt, capped.
const RETRY_BASE_MS = 500;
const RETRY_CAP_MS = 30000;

// 429 and 5xx are worth another attempt; 0 is a request that never got
// an answer, which may or may not have landed. Every other status is
// the caller's fault and will fail identically forever.
function retryable(status: number): boolean {
  return status === 0 || status === 429 || status >= 500;
}

function validate(actor: Actor): void {
  for (const key of Object.keys(actor)) {
    if (!RESERVED_ACTOR_KEYS.includes(key as never)) {
      throw new UnknownActorKeyError(key);
    }
  }
}

// Logger is one identity's view of a client. It is immutable: narrowing
// it returns a new one, so a request-scoped logger cannot disturb the
// one it came from, and concurrent requests cannot overwrite each
// other's identity the way shared client state would.
export class Logger {
  private readonly context: Context;
  private readonly actor: Readonly<Actor>;

  constructor(context: Context, actor: Actor) {
    this.context = context;
    this.actor = Object.freeze({ ...actor });
  }

  log(event: string, metadata: Record<string, unknown> = {}): void {
    this.context.enqueue(this.actor, event, metadata);
  }

  // Adds to this identity without touching it - a request scope built
  // from an organisation scope, say.
  identify(actor: Actor): Logger {
    validate(actor);
    return new Logger(this.context, { ...this.actor, ...actor });
  }
}

export class Context {
  private readonly tenantId: string;
  private readonly batchSize: number;
  private readonly flushIntervalMs: number;
  private readonly maxRetries: number;
  private readonly clock: Clock;
  private readonly transport: Transport;
  private readonly queue: EventQueue;

  private closed = false;
  private timer: unknown = null;
  private inflight = new Set<Promise<void>>();

  constructor(options: Options) {
    this.tenantId = options.tenantId;
    this.batchSize = options.batchSize ?? DEFAULTS.batchSize;
    this.flushIntervalMs = options.flushIntervalMs ?? DEFAULTS.flushIntervalMs;
    this.maxRetries = options.maxRetries ?? DEFAULTS.maxRetries;
    this.clock = options.clock ?? systemClock;
    this.transport =
      options.transport ?? new HttpTransport(options.endpoint ?? DEFAULT_ENDPOINT);
    this.queue = new EventQueue(options.maxQueueSize ?? DEFAULTS.maxQueueSize);
    this.installLifecycle();
  }

  // Opens a scope for whoever the following events belong to. Returns a
  // logger rather than setting client state, so two concurrent requests
  // cannot attribute each other's events.
  identify(actor: Actor): Logger {
    validate(actor);
    return new Logger(this, actor);
  }

  // For events with nobody behind them - a cache warming, a job the
  // process itself did.
  log(event: string, metadata: Record<string, unknown> = {}): void {
    this.enqueue({}, event, metadata);
  }

  /** @internal - the seam Logger writes through. */
  enqueue(actor: Actor, event: string, metadata: Record<string, unknown>): void {
    if (this.closed) {
      this.queue.recordDropped(1);
      return;
    }
    // Stamped here rather than at flush: the event happened now, not
    // whenever the batch happens to drain.
    const entry: Event = {
      timestamp: new Date(this.clock.now()).toISOString(),
      tenant_id: this.tenantId,
      event,
      actor: { ...actor } as Record<string, string>,
      metadata,
    };

    // An entry the server will refuse must never join a batch, or it
    // costs every good event beside it. Encoding here also catches
    // metadata that cannot be serialised at all.
    let encoded: string;
    try {
      encoded = JSON.stringify(entry);
    } catch {
      this.queue.recordDropped(1);
      return;
    }
    if (new TextEncoder().encode(encoded).length > MAX_ENTRY_BYTES) {
      this.queue.recordDropped(1);
      return;
    }

    this.queue.push(entry);

    if (this.queue.size >= this.batchSize) {
      this.background(this.flush());
    } else {
      this.arm();
    }
  }

  // Sends everything pending, retries included.
  async flush(): Promise<void> {
    this.disarm();
    const batch = this.queue.drain();
    if (batch.length === 0) return;
    await this.send(batch, 0);
  }

  // Drains before returning - blocking is the point, since a 200 means
  // the events are stored.
  //
  // One attempt per batch, no retry: this is the caller's shutdown
  // budget, and a second attempt spends time they did not offer.
  // Whatever the attempt does not store is counted in dropped.
  async close(): Promise<void> {
    this.closed = true;
    this.disarm();

    // Work already handed to a background send, which the loop below
    // does not own.
    await this.settled();

    let batch = this.queue.drain();

    while (batch.length > 0) {
      const { status, rejected } = await this.transport.send({ entries: batch });
      if (status >= 200 && status < 300) {
        // The whole batch is stored.
      } else if (status === 400 && rejected !== undefined) {
        // The server named what it refused; the rest was stored.
        this.queue.recordDropped(rejected);
      } else {
        this.queue.recordDropped(batch.length);
      }
      // Whatever arrived while that send was in flight, sent on the same
      // one-attempt terms.
      batch = this.queue.drain();
    }
  }

  get dropped(): number {
    return this.queue.dropped;
  }

  // Resolves once no send is in flight. Pending retry timers are not
  // waited on - those are the clock's business.
  async settled(): Promise<void> {
    while (this.inflight.size > 0) {
      await Promise.all([...this.inflight]);
    }
  }

  private async send(batch: Event[], attempt: number): Promise<void> {
    const { status, rejected } = await this.transport.send({ entries: batch });
    if (status >= 200 && status < 300) return;

    // A reported rejection means the rest of the batch was stored, so
    // only the named entries are lost.
    if (status === 400 && rejected !== undefined) {
      this.queue.recordDropped(rejected);
      return;
    }

    if (retryable(status) && attempt < this.maxRetries) {
      const delay = Math.min(RETRY_BASE_MS * 2 ** attempt, RETRY_CAP_MS);
      // Jitter keeps a fleet of clients from retrying in lockstep.
      const wait = delay / 2 + Math.random() * (delay / 2);
      this.clock.setTimer(wait, () => {
        // The client may have closed while this was armed; the batch is
        // then lost, and saying so beats sending after a drain that
        // already reported itself complete.
        if (this.closed) {
          this.queue.recordDropped(batch.length);
          return;
        }
        this.background(this.send(batch, attempt + 1));
      });
      return;
    }
    this.queue.recordDropped(batch.length);
  }

  private arm(): void {
    if (this.timer !== null) return;
    this.timer = this.clock.setTimer(this.flushIntervalMs, () => {
      this.timer = null;
      this.background(this.flush());
    });
  }

  private disarm(): void {
    if (this.timer === null) return;
    this.clock.clearTimer(this.timer);
    this.timer = null;
  }

  // A browser tab can disappear without warning, and fetch does not
  // outlive it - so the last events of every session need sendBeacon.
  // Not covered by the conformance cases: the runtime owns this path.
  private installLifecycle(): void {
    if (typeof document === "undefined") return;
    if (!(this.transport instanceof HttpTransport)) return;

    const beacon = () => {
      const batch = this.queue.drain();
      if (batch.length === 0) return;
      // Unacknowledged, so a failure here is a lost batch by design.
      (this.transport as HttpTransport).sendBeacon({ entries: batch });
    };
    // visibilitychange is the reliable one; pagehide covers Safari.
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "hidden") beacon();
    });
    window.addEventListener("pagehide", beacon);
  }

  // Keeps a fire-and-forget send awaitable, and swallows its failure so
  // nothing surfaces as an unhandled rejection in the caller.
  private background(promise: Promise<void>): void {
    const pending = promise.catch(() => {}).finally(() => this.inflight.delete(pending));
    this.inflight.add(pending);
  }
}
