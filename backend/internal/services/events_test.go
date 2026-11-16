package services

import (
	"testing"

	"github.com/google/uuid"
)

func TestEventHubFanOutAndUnsubscribe(t *testing.T) {
	hub := NewEventHub()
	user := uuid.New()
	a, cancelA := hub.Subscribe(user)
	b, cancelB := hub.Subscribe(user) // two tabs of the same user
	defer cancelB()

	hub.Publish(Event{Type: EventMessageCreated, UserID: user, AccountID: "1"})
	if got := <-a; got.AccountID != "1" {
		t.Fatalf("a got %+v", got)
	}
	if got := <-b; got.AccountID != "1" {
		t.Fatalf("b got %+v", got)
	}

	cancelA()
	cancelA() // idempotent
	if _, open := <-a; open {
		t.Fatal("expected a closed channel after unsubscribe")
	}
	if hub.Subscribers() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", hub.Subscribers())
	}
}

func TestEventHubDeliversOnlyToOwner(t *testing.T) {
	hub := NewEventHub()
	alice, bob := uuid.New(), uuid.New()
	a, cancelA := hub.Subscribe(alice)
	defer cancelA()
	b, cancelB := hub.Subscribe(bob)
	defer cancelB()

	hub.Publish(Event{Type: EventMessageCreated, UserID: bob, AccountID: "b1", AccountEmail: "b@x", Subject: "secret"})
	if len(a) != 0 {
		t.Fatalf("alice received bob's event: %+v", <-a)
	}
	if got := <-b; got.AccountID != "b1" {
		t.Fatalf("bob got %+v", got)
	}

	// Without an owner the event reaches everyone, as long as it names no account
	hub.Publish(Event{Type: EventSettingsChanged})
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("broadcast not delivered to all: a=%d b=%d", len(a), len(b))
	}
	<-a
	<-b

	// An ownerless event about an account is a bug; it must not be broadcast
	hub.Publish(Event{Type: EventMessageCreated, AccountID: "x", AccountEmail: "x@x", Subject: "leak"})
	if len(a) != 0 || len(b) != 0 {
		t.Fatal("ownerless account event was broadcast")
	}
}

func TestEventHubDropsForSlowSubscriber(t *testing.T) {
	hub := NewEventHub()
	ch, cancel := hub.Subscribe(uuid.New())
	defer cancel()
	for i := 0; i < eventBuffer+10; i++ {
		hub.Publish(Event{Type: EventAccountsChanged}) // must never block
	}
	if len(ch) != eventBuffer {
		t.Fatalf("expected a full buffer of %d, got %d", eventBuffer, len(ch))
	}
}

func TestNilHubPublishIsNoop(t *testing.T) {
	var hub *EventHub
	hub.Publish(Event{Type: EventAccountsChanged})
}
