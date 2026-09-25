package ctxsdk

import (
	"encoding/json"
	"sort"
	"time"
)

// fakeClock starts at Unix epoch 0 so a case can assert exact
// timestamps.
type fakeClock struct {
	now    time.Time
	timers []*fakeTimer
	nextID int
}

type fakeTimer struct {
	id      int
	at      time.Time
	fn      func()
	clock   *fakeClock
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.stopped = true
	t.clock.remove(t.id)
	return true
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.UnixMilli(0).UTC()}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.nextID++
	timer := &fakeTimer{id: c.nextID, at: c.now.Add(d), fn: f, clock: c}
	c.timers = append(c.timers, timer)
	return timer
}

func (c *fakeClock) remove(id int) {
	kept := c.timers[:0]
	for _, t := range c.timers {
		if t.id != id {
			kept = append(kept, t)
		}
	}
	c.timers = kept
}

// Sleep is just time passing, so the cases never actually sleep.
func (c *fakeClock) Sleep(d time.Duration) { c.advance(d) }

// advance lets the whole window happen: a timer fired here may schedule
// another inside it, so the due set is rescanned each pass.
func (c *fakeClock) advance(d time.Duration) {
	target := c.now.Add(d)
	for {
		var due []*fakeTimer
		for _, t := range c.timers {
			if !t.at.After(target) {
				due = append(due, t)
			}
		}
		if len(due) == 0 {
			break
		}
		sort.Slice(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
		first := due[0]
		c.remove(first.id)
		c.now = first.at
		first.fn()
	}
	c.now = target
}

type fakeTransport struct {
	sent   []any
	queued []Result
}

func newFakeTransport(results []Result) *fakeTransport {
	return &fakeTransport{queued: append([]Result{}, results...)}
}

// Records what actually went on the wire, so a case checks the encoded
// JSON rather than the struct behind it.
func (t *fakeTransport) Send(batch Batch) Result {
	body, _ := json.Marshal(batch)
	var generic any
	_ = json.Unmarshal(body, &generic)
	t.sent = append(t.sent, generic)

	if len(t.queued) > 0 {
		result := t.queued[0]
		t.queued = t.queued[1:]
		return result
	}
	return Result{Status: 200}
}
