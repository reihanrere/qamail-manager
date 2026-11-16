package services

import (
	"log"
	"sync"

	"github.com/google/uuid"
)

// Event types pushed to the UI over GET /api/events.
const (
	// EventMessageCreated: new mail arrived (ingest stored it, or a sync saw the count grow).
	EventMessageCreated = "message.created"
	// EventAccountsChanged: account list or counts changed (generate, replace, delete).
	EventAccountsChanged = "accounts.changed"
	// EventSettingsChanged: settings were edited from the Settings page.
	EventSettingsChanged = "settings.changed"
)

// Event is one server-sent event.
//
// UserID is the owner of the account it is about; only that user's streams receive it.
// An event without UserID goes to every stream, so it may not carry account details:
// Publish drops such an event when it names an account, instead of leaking it to all users.
type Event struct {
	Type         string    `json:"type"`
	UserID       uuid.UUID `json:"-"`
	AccountID    string    `json:"accountId,omitempty"`
	AccountEmail string    `json:"accountEmail,omitempty"`
	Subject      string    `json:"subject,omitempty"`
}

// eventBuffer is how many events a slow subscriber may lag behind before events are
// dropped for it; the UI re-fetches on its polling interval anyway.
const eventBuffer = 32

// EventHub fans out events to the connected UIs of the user they concern.
type EventHub struct {
	mu          sync.Mutex
	subscribers map[chan Event]uuid.UUID
}

// NewEventHub builds an empty hub.
func NewEventHub() *EventHub {
	return &EventHub{subscribers: map[chan Event]uuid.UUID{}}
}

// Subscribe returns userID's channel of events and a function that unsubscribes and closes it.
func (h *EventHub) Subscribe(userID uuid.UUID) (<-chan Event, func()) {
	ch := make(chan Event, eventBuffer)
	h.mu.Lock()
	h.subscribers[ch] = userID
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// Publish delivers the event to its owner's subscribers (or, without an owner, to all)
// without blocking; a full subscriber misses the event rather than stalling the publisher.
func (h *EventHub) Publish(event Event) {
	if h == nil {
		return
	}
	broadcast := event.UserID == uuid.Nil
	if broadcast && (event.AccountID != "" || event.AccountEmail != "" || event.Subject != "") {
		log.Printf("events: dropped %s event about account %s without an owner", event.Type, event.AccountID)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, userID := range h.subscribers {
		if !broadcast && userID != event.UserID {
			continue
		}
		select {
		case ch <- event:
		default:
		}
	}
}

// Subscribers reports how many UIs are connected (for tests and diagnostics).
func (h *EventHub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}
