package tcpbackoff

import (
	"errors"
	"testing"
	"time"
)

func TestBackoffDoublesAndResets(t *testing.T) {
	tr := New()
	now := time.Unix(0, 0)
	tr.now = func() time.Time { return now }

	if !tr.Begin("a") {
		t.Fatal("first Begin must succeed")
	}
	if tr.Begin("a") {
		t.Fatal("a second concurrent dial to the same key must be refused")
	}
	if !tr.Begin("b") {
		t.Fatal("other keys are independent")
	}
	tr.End("b", nil)

	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute}
	for i, w := range want {
		if d := tr.End("a", errors.New("x")); d != w {
			t.Fatalf("failure %d: delay %v want %v", i+1, d, w)
		}
		if tr.Begin("a") {
			t.Fatalf("failure %d: Begin must be refused inside the backoff window", i+1)
		}
		now = now.Add(w) // window over
		if !tr.Begin("a") {
			t.Fatalf("failure %d: Begin must succeed after the window", i+1)
		}
	}

	// Cap at one hour no matter how many failures.
	for i := 0; i < 20; i++ {
		tr.End("a", errors.New("x"))
		now = now.Add(2 * time.Hour)
		tr.Begin("a")
	}
	if d := tr.End("a", errors.New("x")); d != time.Hour {
		t.Fatalf("cap: %v", d)
	}

	now = now.Add(2 * time.Hour)
	tr.Begin("a")
	tr.End("a", nil)
	if !tr.Begin("a") {
		t.Fatal("success must clear the backoff")
	}
	tr.End("a", nil)
	tr.Begin("a")
	if d := tr.End("a", errors.New("x")); d != 30*time.Second {
		t.Fatalf("restart delay %v", d)
	}
}

func TestNilTrackerIsPermissive(t *testing.T) {
	var tr *Tracker
	if !tr.Begin("a") || tr.End("a", errors.New("x")) != 0 {
		t.Fatal("nil tracker must be a no-op")
	}
	tr.Reset()
}
