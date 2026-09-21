package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// SSEBroker handles real-time Server-Sent Events
type SSEBroker struct {
	clients map[chan []byte]bool
	mu      sync.Mutex
}

// NewSSEBroker creates a new broker
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[chan []byte]bool),
	}
}

func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	messageChan := make(chan []byte, 32)

	b.mu.Lock()
	b.clients[messageChan] = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, messageChan)
		b.mu.Unlock()
		close(messageChan)
	}()

	// Send initial ping
	if _, err := fmt.Fprintf(w, "data: {\"type\":\"connected\"}\n\n"); err != nil {
		return
	}
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg, ok := <-messageChan:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// Broadcast sends a JSON message to all connected SSE clients
func (b *SSEBroker) Broadcast(event interface{}) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.clients {
		select {
		case ch <- data:
		default:
			// Non-blocking drop if channel buffer full
		}
	}
}
