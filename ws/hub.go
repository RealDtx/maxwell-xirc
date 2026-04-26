package ws

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/RealDtx/maxwell-irc/irc"
)

type Hub struct {
	mu      sync.RWMutex
	bus     *irc.EventBus
	eventCh <-chan irc.Event
	clients map[chan []byte]struct{}
	stopCh  chan struct{}
}

func NewHub(bus *irc.EventBus) *Hub {
	return &Hub{
		bus:     bus,
		clients: make(map[chan []byte]struct{}),
		stopCh:  make(chan struct{}),
	}
}

func (h *Hub) Start() {
	h.eventCh = h.bus.Subscribe()
	go h.loop()
}

func (h *Hub) Stop() {
	close(h.stopCh)
	h.bus.Unsubscribe(h.eventCh)
}

func (h *Hub) Register(ch chan []byte) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Unregister(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) loop() {
	for {
		select {
		case <-h.stopCh:
			return
		case ev, ok := <-h.eventCh:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				log.Printf("failed to marshal event: %v", err)
				continue
			}
			h.broadcast(data)
		}
	}
}

func (h *Hub) broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.clients {
		select {
		case ch <- data:
		default:
			// Slow client, drop
		}
	}
}
