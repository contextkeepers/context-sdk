package ctxsdk

// eventQueue is bounded on purpose: an unreachable sink must cost a
// fixed amount of memory, and recent events are worth more than old
// ones.
type eventQueue struct {
	events  []Event
	dropped int
	maxSize int
}

func newEventQueue(maxSize int) *eventQueue {
	return &eventQueue{maxSize: maxSize}
}

func (q *eventQueue) push(event Event) {
	q.events = append(q.events, event)
	for len(q.events) > q.maxSize {
		q.events = q.events[1:]
		q.dropped++
	}
}

func (q *eventQueue) drain() []Event {
	taken := q.events
	q.events = nil
	return taken
}

func (q *eventQueue) size() int { return len(q.events) }
