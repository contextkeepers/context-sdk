package ctxsdk

import "sync"

// goroutineDispatcher runs sends on one worker, so Log hands off
// instead of waiting on HTTP.
type goroutineDispatcher struct {
	work chan func()
	wg   sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

func newGoroutineDispatcher() *goroutineDispatcher {
	d := &goroutineDispatcher{work: make(chan func(), 256)}
	go d.run()
	return d
}

func (d *goroutineDispatcher) run() {
	for fn := range d.work {
		fn()
		d.wg.Done()
	}
}

// Submit after Shutdown is dropped rather than panicking on a closed
// channel. A timer armed before shutdown can always fire after it, and
// a logging client must not take the process down.
func (d *goroutineDispatcher) Submit(fn func()) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.wg.Add(1)
	d.mu.Unlock()
	d.work <- fn
}

func (d *goroutineDispatcher) Drain() { d.wg.Wait() }

func (d *goroutineDispatcher) Shutdown() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.mu.Unlock()

	d.wg.Wait()
	close(d.work)
}

// InlineDispatcher runs the work where it was submitted - what the
// conformance cases use, so a case never depends on scheduling.
type InlineDispatcher struct{}

func (InlineDispatcher) Submit(fn func()) { fn() }
func (InlineDispatcher) Drain()           {}
func (InlineDispatcher) Shutdown()        {}
