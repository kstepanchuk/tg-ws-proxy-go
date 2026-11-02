package cflabels

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"tg-ws-proxy/internal/telegram"
)

func setLabels(t *testing.T, m map[int][]string) {
	t.Helper()
	old := telegram.CFHostLabels
	telegram.CFHostLabels = m
	t.Cleanup(func() { telegram.CFHostLabels = old })
}

func TestHostsOrderAndFailover(t *testing.T) {
	setLabels(t, map[int][]string{2: {"ember", "cedar", "birch"}})
	p := New()
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }

	want := []string{"ember.x.ru", "cedar.x.ru", "birch.x.ru"}
	if got := p.Hosts("x.ru", 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("initial order %v", got)
	}

	// The preferred label fails: the spares come first, the failed one last.
	if d := p.Report("ember.x.ru", errors.New("timeout")); d != 10*time.Minute {
		t.Fatalf("first cooldown %v", d)
	}
	if got := p.Hosts("x.ru", 2); !reflect.DeepEqual(got, []string{"cedar.x.ru", "birch.x.ru", "ember.x.ru"}) {
		t.Fatalf("after one failure %v", got)
	}

	// Everything fails: still returns all hosts, the soonest-to-recover first.
	p.Report("cedar.x.ru", errors.New("x"))
	now = now.Add(time.Minute)
	p.Report("birch.x.ru", errors.New("x"))
	got := p.Hosts("x.ru", 2)
	if len(got) != 3 || got[0] != "ember.x.ru" {
		t.Fatalf("all cooling must still list every host, soonest first: %v", got)
	}

	// Cooldown expires -> usable again, original order.
	now = now.Add(11 * time.Minute)
	if got := p.Hosts("x.ru", 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("after expiry %v", got)
	}

	// Success clears history so the next failure starts from the initial delay.
	p.Report("ember.x.ru", nil)
	if d := p.Report("ember.x.ru", errors.New("x")); d != 10*time.Minute {
		t.Fatalf("cooldown after reset %v", d)
	}
}

func TestCooldownDoublesAndCaps(t *testing.T) {
	setLabels(t, nil)
	p := New()
	var last time.Duration
	for i := 0; i < 20; i++ {
		last = p.Report("kws2.x.ru", errors.New("x"))
	}
	if last != DefaultMax {
		t.Fatalf("cooldown must cap at %v, got %v", DefaultMax, last)
	}
	if d := New().Report("a.x.ru", errors.New("x")); d != DefaultInitial {
		t.Fatalf("initial %v", d)
	}
}

func TestDefaultsToPrefixAndNilPool(t *testing.T) {
	setLabels(t, nil)
	old := telegram.CFHostPrefix
	telegram.CFHostPrefix = "cdn"
	defer func() { telegram.CFHostPrefix = old }()

	var nilPool *Pool
	if got := nilPool.Hosts("x.ru", 4); !reflect.DeepEqual(got, []string{"cdn4.x.ru"}) {
		t.Fatalf("nil pool must behave statically: %v", got)
	}
	if nilPool.Report("cdn4.x.ru", errors.New("x")) != 0 || nilPool.Cooling() != 0 {
		t.Fatal("nil pool must be a no-op")
	}
}
