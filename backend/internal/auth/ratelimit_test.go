package auth

import (
	"sync"
	"testing"
	"time"
)

func TestLoginLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := NewLoginLimiter(3, 10*time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := l.Attempt("ip|a@example.com"); !ok {
			t.Fatalf("attempt %d refused before the limit", i+1)
		}
	}
	ok, retry := l.Attempt("ip|a@example.com")
	if ok || retry != 10*time.Minute {
		t.Fatalf("ok=%v retry=%v, want refused for the full window", ok, retry)
	}
	if ok, _ := l.Attempt("ip|b@example.com"); !ok {
		t.Fatal("another email must not be blocked")
	}

	now = now.Add(10*time.Minute + time.Second)
	if ok, _ := l.Attempt("ip|a@example.com"); !ok {
		t.Fatal("still blocked after the window passed")
	}
}

func TestLoginLimiterReset(t *testing.T) {
	l := NewLoginLimiter(1, time.Hour)
	l.Attempt("k")
	l.Reset("k")
	if ok, _ := l.Attempt("k"); !ok {
		t.Fatal("blocked after reset")
	}
}

func TestLoginLimiterUndoGivesTheAttemptBack(t *testing.T) {
	l := NewLoginLimiter(1, time.Hour)
	l.Attempt("k")
	l.Undo("k")
	if ok, _ := l.Attempt("k"); !ok {
		t.Fatal("an undone attempt still counted")
	}
	l.Undo("never-seen") // must not panic
}

func TestLoginLimiterSlidingWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := NewLoginLimiter(2, 10*time.Minute)
	l.now = func() time.Time { return now }

	l.Attempt("k")
	now = now.Add(6 * time.Minute)
	l.Attempt("k")
	_, retry := l.Attempt("k")
	if retry != 4*time.Minute {
		t.Fatalf("retry = %v, want 4m (the oldest attempt expires first)", retry)
	}
	now = now.Add(5 * time.Minute) // first attempt expired, second still counts
	if ok, _ := l.Attempt("k"); !ok {
		t.Fatal("expected one attempt left, below the limit")
	}
}

// Checking and counting must be one step: a burst of concurrent attempts may not get more
// than the limit through just because none of them has been counted yet.
func TestLoginLimiterAttemptIsAtomic(t *testing.T) {
	const max, burst = 5, 200
	l := NewLoginLimiter(max, time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	start := make(chan struct{})
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _ := l.Attempt("k"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if allowed != max {
		t.Fatalf("%d attempts allowed, want exactly %d", allowed, max)
	}
}
