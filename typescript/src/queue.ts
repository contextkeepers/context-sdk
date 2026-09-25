import type { Event } from "./types.ts";

// Bounded on purpose: an unreachable sink must cost a fixed amount of
// memory, and recent events are worth more than old ones.
export class EventQueue {
  private events: Event[] = [];
  private droppedCount = 0;
  private readonly maxSize: number;

  constructor(maxSize: number) {
    this.maxSize = maxSize;
  }

  push(event: Event): void {
    this.events.push(event);
    while (this.events.length > this.maxSize) {
      this.events.shift();
      this.droppedCount++;
    }
  }

  // Hands over everything pending; the caller owns it from here.
  drain(): Event[] {
    const taken = this.events;
    this.events = [];
    return taken;
  }

  get size(): number {
    return this.events.length;
  }

  get dropped(): number {
    return this.droppedCount;
  }

  recordDropped(n: number): void {
    this.droppedCount += n;
  }
}
