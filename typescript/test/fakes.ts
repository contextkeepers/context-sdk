import type { Batch, Clock, Response, Transport } from "../src/types.ts";

interface Timer {
  id: number;
  at: number;
  fn: () => void;
}

// Starts at Unix epoch 0 so a case can assert exact timestamps.
export class FakeClock implements Clock {
  private current = 0;
  private timers: Timer[] = [];
  private nextId = 1;

  now(): number {
    return this.current;
  }

  setTimer(delayMs: number, fn: () => void): unknown {
    const id = this.nextId++;
    this.timers.push({ id, at: this.current + delayMs, fn });
    return id;
  }

  clearTimer(handle: unknown): void {
    this.timers = this.timers.filter((t) => t.id !== handle);
  }

  // A blocking wait is just time passing, so the cases never sleep.
  async sleep(delayMs: number): Promise<void> {
    await this.advance(delayMs);
  }

  // Fires everything due in time order, moving the clock as it goes, so
  // a timer set by a timer sees the right now().
  //
  // `settle` runs between firings because a timer's work is async: a
  // retry schedules the next retry only after its request resolves, so
  // without draining in between the window would end early.
  async advance(ms: number, settle?: () => Promise<void>): Promise<void> {
    const target = this.current + ms;
    for (;;) {
      if (settle) await settle();
      const due = this.timers
        .filter((t) => t.at <= target)
        .sort((a, b) => a.at - b.at)[0];
      if (!due) break;
      this.timers = this.timers.filter((t) => t.id !== due.id);
      this.current = due.at;
      due.fn();
    }
    this.current = target;
  }
}

export class FakeTransport implements Transport {
  readonly sent: Batch[] = [];
  private queued: Response[];

  constructor(responses: Response[] = []) {
    this.queued = [...responses];
  }

  async send(batch: Batch): Promise<Response> {
    this.sent.push(structuredClone(batch));
    return this.queued.shift() ?? { status: 200 };
  }
}
