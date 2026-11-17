package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Broker fans out change notifications to connected SSE clients.
type Broker struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func NewBroker() *Broker { return &Broker{subs: map[chan struct{}]struct{}{}} }

func (b *Broker) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) unsubscribe(ch chan struct{}) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

// Notify signals every subscriber. Pending signals are coalesced, so a slow
// client never blocks the writer.
func (b *Broker) Notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// events: GET /api/events — Server-Sent Events stream; emits "change" whenever tasks are modified.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.broker.subscribe()
	defer s.broker.unsubscribe(ch)

	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprint(w, "event: change\ndata: {}\n\n")
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		flusher.Flush()
	}
}
