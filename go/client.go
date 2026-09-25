package ctxsdk

import (
	"encoding/json"
	"math/rand"
	"sync"
	"time"
)

// Server defaults: a backend trades a longer window for fewer round
// trips, where a browser would rather not lose a session.
const (
	DefaultBatchSize     = 500
	DefaultFlushInterval = 5 * time.Second
	DefaultMaxQueueSize  = 10000
	DefaultMaxRetries    = 3
	// Inside ECS's 30s default grace period, with room for the app's
	// own shutdown work.
	DefaultCloseTimeout = 5 * time.Second

	retryBase = 500 * time.Millisecond
	retryCap  = 30 * time.Second
)

// Options configures a Client. Only TenantID is required.
type Options struct {
	TenantID      string
	Endpoint      string
	BatchSize     int
	FlushInterval time.Duration
	MaxQueueSize  int
	MaxRetries    int
	// How long Close may spend draining. Must be comfortably shorter
	// than the container's grace period: a drain that outlives it is
	// SIGKILLed mid-send and loses everything it was holding.
	CloseTimeout time.Duration
	Clock        Clock
	Transport    Transport
	Dispatcher   Dispatcher
}

type Client struct {
	tenantID      string
	batchSize     int
	flushInterval time.Duration
	maxRetries    int
	closeTimeout  time.Duration
	clock         Clock
	transport     Transport
	dispatcher    Dispatcher

	mu     sync.Mutex
	queue  *eventQueue
	closed bool
	timer  Timer
}

func New(options Options) *Client {
	if options.BatchSize == 0 {
		options.BatchSize = DefaultBatchSize
	}
	if options.FlushInterval == 0 {
		options.FlushInterval = DefaultFlushInterval
	}
	if options.MaxQueueSize == 0 {
		options.MaxQueueSize = DefaultMaxQueueSize
	}
	if options.MaxRetries == 0 {
		options.MaxRetries = DefaultMaxRetries
	}
	if options.CloseTimeout == 0 {
		options.CloseTimeout = DefaultCloseTimeout
	}
	if options.Endpoint == "" {
		options.Endpoint = DefaultEndpoint
	}
	if options.Clock == nil {
		options.Clock = systemClock{}
	}
	if options.Transport == nil {
		options.Transport = NewHTTPTransport(options.Endpoint)
	}
	if options.Dispatcher == nil {
		options.Dispatcher = newGoroutineDispatcher()
	}

	return &Client{
		tenantID:      options.TenantID,
		batchSize:     options.BatchSize,
		flushInterval: options.FlushInterval,
		maxRetries:    options.MaxRetries,
		closeTimeout:  options.CloseTimeout,
		clock:         options.Clock,
		transport:     options.Transport,
		dispatcher:    options.Dispatcher,
		queue:         newEventQueue(options.MaxQueueSize),
	}
}

// Logger is one identity's view of a client.
//
// Immutable: narrowing returns a new Logger, so a request-scoped logger
// cannot disturb the one it came from, and two goroutines cannot
// overwrite each other's identity the way shared client state would.
type Logger struct {
	client *Client
	actor  map[string]string
}

// Log records an event against this scope's identity.
func (l *Logger) Log(event string, metadata map[string]any) {
	l.client.enqueue(l.actor, event, metadata)
}

// Identify adds to this identity without touching it.
func (l *Logger) Identify(fields ...ActorField) *Logger {
	return &Logger{client: l.client, actor: merge(l.actor, fields)}
}

// Identify opens a scope for whoever the following events belong to.
// It returns a logger rather than setting client state, so two
// concurrent requests cannot attribute each other's events.
func (c *Client) Identify(fields ...ActorField) *Logger {
	return &Logger{client: c, actor: merge(nil, fields)}
}

func merge(base map[string]string, fields []ActorField) map[string]string {
	out := make(map[string]string, len(base)+len(fields))
	for key, value := range base {
		out[key] = value
	}
	for _, field := range fields {
		// Only a deliberately constructed zero ActorField reaches this.
		if field.key != "" {
			out[field.key] = field.value
		}
	}
	return out
}

// Log records an event with nobody behind it - a cache warming, a job
// the process itself did.
func (c *Client) Log(event string, metadata map[string]any) {
	c.enqueue(nil, event, metadata)
}

// enqueue is the seam a Logger writes through. It returns immediately
// and never fails - a logging client that breaks the caller is worse
// than one that loses an event.
func (c *Client) enqueue(actor map[string]string, event string, metadata map[string]any) {
	if metadata == nil {
		metadata = map[string]any{}
	}

	// Stamped here rather than at flush: the event happened now, not
	// whenever the batch happens to drain.
	entry := Event{
		Timestamp: timestamp(c.clock.Now()),
		TenantID:  c.tenantID,
		Event:     event,
		Actor:     copyActor(actor),
		Metadata:  metadata,
	}

	c.mu.Lock()
	if c.closed {
		c.queue.dropped++
		c.mu.Unlock()
		return
	}
	// An entry the server will refuse must never join a batch, or it
	// costs every good event beside it. Encoding here also catches
	// metadata that cannot be serialised at all.
	encoded, err := json.Marshal(entry)
	if err != nil || len(encoded) > MaxEntryBytes {
		c.queue.dropped++
		c.mu.Unlock()
		return
	}

	c.queue.push(entry)
	full := c.queue.size() >= c.batchSize
	c.mu.Unlock()

	if full {
		c.dispatcher.Submit(c.Flush)
	} else {
		c.arm()
	}
}

// Flush sends everything pending, retries included.
func (c *Client) Flush() {
	c.disarm()
	c.mu.Lock()
	batch := c.queue.drain()
	c.mu.Unlock()
	if len(batch) > 0 {
		c.send(batch, 0)
	}
}

// Close drains before returning - blocking is the point, since a 200
// means the events are stored. Bounded by CloseTimeout, because a drain
// that outlives the grace period is SIGKILLed and loses everything it
// was holding.
//
// It returns *ShutdownIncompleteError when it had to abandon events, so
// a deploy that quietly loses a batch per instance is visible.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.disarm()

	deadline := c.clock.Now().Add(c.closeTimeout)
	// Work already handed to a background send, which the loop below
	// does not own.
	c.dispatcher.Drain()

	abandoned := 0
	attempt := 0

	c.mu.Lock()
	batch := c.queue.drain()
	c.mu.Unlock()

	for len(batch) > 0 {
		result := c.transport.Send(Batch{Entries: batch})
		status := result.Status
		if status >= 200 && status < 300 {
			c.mu.Lock()
			batch = c.queue.drain()
			c.mu.Unlock()
			attempt = 0
			continue
		}
		if status == 400 && result.Reported {
			abandoned += result.Rejected
			c.mu.Lock()
			c.queue.dropped += result.Rejected
			batch = c.queue.drain()
			c.mu.Unlock()
			attempt = 0
			continue
		}
		if !retryable(status) || attempt >= c.maxRetries {
			abandoned += len(batch)
			c.mu.Lock()
			c.queue.dropped += len(batch)
			c.mu.Unlock()
			break
		}
		wait := retryBase << attempt
		if wait > retryCap {
			wait = retryCap
		}
		if c.clock.Now().Add(wait).After(deadline) {
			abandoned += len(batch)
			c.mu.Lock()
			c.queue.dropped += len(batch)
			c.mu.Unlock()
			break
		}
		c.clock.Sleep(wait)
		attempt++
	}

	c.dispatcher.Shutdown()
	if abandoned > 0 {
		return &ShutdownIncompleteError{Abandoned: abandoned}
	}
	return nil
}

// Dropped counts events discarded for a full queue, a dead sink, or a
// batch the server refused.
func (c *Client) Dropped() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queue.dropped
}

func (c *Client) send(batch []Event, attempt int) {
	result := c.transport.Send(Batch{Entries: batch})
	status := result.Status
	if status >= 200 && status < 300 {
		return
	}

	// A reported rejection means the rest of the batch was stored, so
	// only the named entries are lost.
	if status == 400 && result.Reported {
		c.mu.Lock()
		c.queue.dropped += result.Rejected
		c.mu.Unlock()
		return
	}

	if retryable(status) && attempt < c.maxRetries {
		delay := retryBase << attempt
		if delay > retryCap {
			delay = retryCap
		}
		// Jitter keeps a fleet of clients from retrying in lockstep.
		wait := delay/2 + time.Duration(rand.Int63n(int64(delay/2)))
		c.clock.AfterFunc(wait, func() {
			// The client may have closed while this was armed; the batch
			// is then lost, and saying so beats retrying into a shut
			// down dispatcher.
			c.mu.Lock()
			closed := c.closed
			if closed {
				c.queue.dropped += len(batch)
			}
			c.mu.Unlock()
			if closed {
				return
			}
			c.dispatcher.Submit(func() { c.send(batch, attempt+1) })
		})
		return
	}

	c.mu.Lock()
	c.queue.dropped += len(batch)
	c.mu.Unlock()
}

func (c *Client) arm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		return
	}
	c.timer = c.clock.AfterFunc(c.flushInterval, func() {
		c.mu.Lock()
		c.timer = nil
		c.mu.Unlock()
		c.dispatcher.Submit(c.Flush)
	})
}

func (c *Client) disarm() {
	c.mu.Lock()
	timer := c.timer
	c.timer = nil
	c.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}

// retryable reports whether another attempt could succeed. 0 is a
// request that got no answer; every other status outside these will
// fail identically forever.
func retryable(status int) bool {
	return status == 0 || status == 429 || status >= 500
}

func copyActor(actor map[string]string) map[string]string {
	out := make(map[string]string, len(actor))
	for k, v := range actor {
		out[k] = v
	}
	return out
}
