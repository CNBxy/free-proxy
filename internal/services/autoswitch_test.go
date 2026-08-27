package services

import (
	"testing"
	"time"
)

// The first reconnect is immediate — one unexpected exit is ordinary and
// recovering fast is the point. What follows has to grow, because the loop it
// replaces forked OpenVPN and rewrote the policy routes as fast as the node
// could drop.
func TestReconnectBackoffGrowsAndCaps(t *testing.T) {
	if got := reconnectBackoff(1); got != 0 {
		t.Errorf("first attempt: got %v, want 0", got)
	}
	if got := reconnectBackoff(0); got != 0 {
		t.Errorf("streak 0: got %v, want 0", got)
	}

	prev := time.Duration(0)
	for streak := 2; streak <= 20; streak++ {
		got := reconnectBackoff(streak)
		if got <= 0 {
			t.Fatalf("streak %d: got %v, want a positive delay", streak, got)
		}
		if got < prev {
			t.Fatalf("streak %d: delay shrank from %v to %v", streak, prev, got)
		}
		if got > maxReconnectBackoff {
			t.Fatalf("streak %d: delay %v exceeds the cap %v", streak, got, maxReconnectBackoff)
		}
		prev = got
	}
	if prev != maxReconnectBackoff {
		t.Errorf("a long streak settled at %v, want the cap %v", prev, maxReconnectBackoff)
	}
}

func TestNoteUnexpectedExitTracksStreak(t *testing.T) {
	s := &AutoSwitchService{}

	if got := s.noteUnexpectedExit(); got != 0 {
		t.Errorf("first exit: got %v, want no delay", got)
	}
	second := s.noteUnexpectedExit()
	third := s.noteUnexpectedExit()
	if second <= 0 || third <= second {
		t.Errorf("consecutive exits did not back off: %v then %v", second, third)
	}

	// A tunnel that survives the window puts the next exit back at the start.
	s.mu.Lock()
	s.lastExitAt = time.Now().Add(-2 * reconnectStreakWindow)
	s.mu.Unlock()
	if got := s.noteUnexpectedExit(); got != 0 {
		t.Errorf("exit after a quiet window: got %v, want no delay", got)
	}
}
