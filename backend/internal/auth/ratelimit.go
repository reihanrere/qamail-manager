package auth

import (
	"sync"
	"time"
)

// maxTrackedKeys bounds the limiter's memory when someone sprays random emails.
const maxTrackedKeys = 100_000

// LoginLimiter counts login tries per key (client IP plus email) inside a sliding
// window; a successful login forgets them. It lives in memory: a restart clears it, which is
// acceptable for a brute-force guard on a single backend instance.
type LoginLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time
}

// NewLoginLimiter allows max failures per window for each key.
func NewLoginLimiter(max int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{max: max, window: window, now: time.Now, failures: map[string][]time.Time{}}
}

// Attempt counts one login try for key and reports whether it may go ahead. Checking and
// counting happen in one step, so a burst of concurrent requests cannot all pass before the
// first failure is recorded. A refused try is not counted; retryAfter is then how long until
// the oldest counted try leaves the window. A successful login calls Reset, a try that never
// reached the password check calls Undo.
func (l *LoginLimiter) Attempt(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.recent(key)
	if len(recent) >= l.max {
		return false, recent[0].Add(l.window).Sub(l.now())
	}
	if _, tracked := l.failures[key]; !tracked && len(l.failures) >= maxTrackedKeys {
		l.sweep()
	}
	l.failures[key] = append(recent, l.now())
	return true, 0
}

// Undo takes back the newest try of key, for a try that was cut short before any password
// was checked (the server was busy), so it must not count against the user.
func (l *LoginLimiter) Undo(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if times := l.recent(key); len(times) > 1 {
		l.failures[key] = times[:len(times)-1]
	} else {
		delete(l.failures, key)
	}
}

// Reset forgets key, after a successful login.
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// recent returns the failures of key still inside the window, dropping the older ones.
func (l *LoginLimiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	times := l.failures[key]
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	times = times[i:]
	if len(times) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = times
	return times
}

// sweep drops every key whose failures have all expired.
func (l *LoginLimiter) sweep() {
	for key := range l.failures {
		l.recent(key)
	}
}
