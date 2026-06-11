package sse

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

type SSEManager struct {
	mu      sync.RWMutex
	clients map[string]map[chan []byte]struct{}
	logger  *slog.Logger
}

func NewSSEManager(logger *slog.Logger) *SSEManager {
	return &SSEManager{
		logger:  logger,
		clients: make(map[string]map[chan []byte]struct{}),
	}
}

func (m *SSEManager) Subscribe(
	bookingCode string,
) chan []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan []byte, 10)

	if _, exists := m.clients[bookingCode]; !exists {
		m.clients[bookingCode] = make(map[chan []byte]struct{})
	}

	m.clients[bookingCode][ch] = struct{}{}

	subscribeMsg := fmt.Sprintf("Customer with Booking Code %v just subscribed", bookingCode)

	m.logger.Info(subscribeMsg)

	return ch
}

func (m *SSEManager) Unsubscribe(
	bookingCode string,
	ch chan []byte,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if listeners, exists := m.clients[bookingCode]; exists {
		delete(listeners, ch)

		if len(listeners) == 0 {
			delete(m.clients, bookingCode)
		}
	}

	unSubscribeMsg := fmt.Sprintf("Customer with Booking Code %v just unsubscribed", bookingCode)

	m.logger.Info(unSubscribeMsg)

	close(ch)
}

func (m *SSEManager) Send(
	bookingCode string,
	data any,
) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	listeners := m.clients[bookingCode]

	payload, _ := json.Marshal(data)

	m.logger.Info("Payload sent back to the client", bookingCode, data)

	for ch := range listeners {
		select {
		case ch <- payload:
		default:
			// skip slow consumers
		}
	}
}

func (s *SSEManager) StreamBookingClient(c *gin.Context, bookingCode string) {

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	ch := s.Subscribe(bookingCode)
	defer s.Unsubscribe(bookingCode, ch)

	ctx := c.Request.Context()

	// Optional: Send initial connection event
	fmt.Fprintf(c.Writer, "event: connected\n")
	fmt.Fprintf(c.Writer, "data: connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return

		case msg, ok := <-ch:
			if !ok {
				return
			}

			fmt.Fprintf(c.Writer, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
