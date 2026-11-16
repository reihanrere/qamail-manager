package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/reihanrere/qamail-manager/internal/services"
)

// EventsHandler streams server-sent events so the UI shows new mail without polling.
type EventsHandler struct {
	hub       *services.EventHub
	auth      *services.AuthService
	heartbeat time.Duration
}

// sessionCheckTimeout bounds one session re-check so a slow database cannot hang a stream.
const sessionCheckTimeout = 5 * time.Second

// NewEventsHandler builds the SSE handler. heartbeat keeps idle connections open
// through proxies (nginx closes reads after 60s by default). auth re-validates the
// session behind each stream, because RequireAuth only runs when the stream starts.
func NewEventsHandler(hub *services.EventHub, auth *services.AuthService, heartbeat time.Duration) *EventsHandler {
	return &EventsHandler{hub: hub, auth: auth, heartbeat: heartbeat}
}

// sessionStillValid reports whether token still maps to an active session. A database
// error counts as invalid: the stream closes and the browser reconnects through RequireAuth.
func (h *EventsHandler) sessionStillValid(token string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), sessionCheckTimeout)
	defer cancel()
	_, err := h.auth.Authenticate(ctx, token)
	return err == nil
}

// Stream handles GET /api/events.
func (h *EventsHandler) Stream(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")
	// Tells nginx not to buffer the stream
	c.Set("X-Accel-Buffering", "no")

	// Only events about this user's accounts (and ownerless broadcasts) reach the stream
	events, unsubscribe := h.hub.Subscribe(userID(c))
	// The request context is gone once the writer runs, so keep the token for the re-checks
	token := c.Cookies(SessionCookieName)
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer unsubscribe()
		ticker := time.NewTicker(h.heartbeat)
		defer ticker.Stop()

		// Sent at once so the client knows the stream is live
		if writeComment(w, "connected") != nil {
			return
		}
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				// Logout, passwd and disable only delete session rows, so check before every event
				if !h.sessionStillValid(token) {
					return
				}
				if writeEvent(w, event) != nil {
					return // client went away
				}
			case <-ticker.C:
				if !h.sessionStillValid(token) {
					return // idle stream of a revoked or expired session
				}
				if writeComment(w, "ping") != nil {
					return
				}
			}
		}
	})
}

func writeEvent(w *bufio.Writer, event services.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
		return err
	}
	return w.Flush()
}

func writeComment(w *bufio.Writer, text string) error {
	if _, err := fmt.Fprintf(w, ": %s\n\n", text); err != nil {
		return err
	}
	return w.Flush()
}
