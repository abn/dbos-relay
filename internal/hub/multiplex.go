package hub

import (
	"sync"

	"github.com/abn/relay/internal/protocol"
)

// Multiplexer handles routing responses to waiting requesters based on request IDs.
type Multiplexer struct {
	mu      sync.Mutex
	pending map[string]chan protocol.Message
	closed  bool
}

func NewMultiplexer() *Multiplexer {
	return &Multiplexer{
		pending: make(map[string]chan protocol.Message),
	}
}

// Register creates a channel to await a response for a given request ID.
// The returned function should be called to clean up the channel, typically in a defer.
func (m *Multiplexer) Register(requestID string) (<-chan protocol.Message, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan protocol.Message, 1)
	if m.closed {
		close(ch)
		return ch, func() {}
	}

	m.pending[requestID] = ch

	unregister := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if c, ok := m.pending[requestID]; ok {
			delete(m.pending, requestID)
			close(c)
		}
	}

	return ch, unregister
}

// RouteResponse attempts to route a message to the waiting requester.
// Returns true if the message was routed, false otherwise.
func (m *Multiplexer) RouteResponse(msg protocol.Message) bool {
	if msg == nil {
		return false
	}
	reqID := msg.GetRequestID()
	if reqID == "" {
		return false
	}

	m.mu.Lock()
	ch, ok := m.pending[reqID]
	if ok {
		delete(m.pending, reqID)
	}
	m.mu.Unlock()

	if ok {
		ch <- msg
		close(ch)
		return true
	}

	return false
}

// CancelAll cancels all pending requests, typically when a connection closes.
func (m *Multiplexer) CancelAll(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	for id, ch := range m.pending {
		delete(m.pending, id)
		close(ch)
	}
}
