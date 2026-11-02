// Package cflabels keeps a pool of Cloudflare host names per DC and rotates
// away from names the network starts to filter.
//
// ISPs block TLS by SNI, and they block names that carry a lot of Telegram
// traffic within hours. A DC therefore gets several interchangeable labels
// (all pointing at the same Telegram front-end through Cloudflare); a label
// whose connections fail goes into cooldown (10 min, doubling to 6 h) and the
// next label is used, so the proxy keeps working without anyone editing DNS.
package cflabels

import (
	"sort"
	"sync"
	"time"

	"tg-ws-proxy/internal/telegram"
)

const (
	DefaultInitial = 10 * time.Minute
	DefaultMax     = 6 * time.Hour
	maxDoublings   = 10
)

type state struct {
	failures int
	until    time.Time
}

type Pool struct {
	mu      sync.Mutex
	now     func() time.Time
	initial time.Duration
	max     time.Duration
	hosts   map[string]*state
}

func New() *Pool {
	return &Pool{now: time.Now, initial: DefaultInitial, max: DefaultMax, hosts: map[string]*state{}}
}

// Hosts returns the host names to try for dc under baseDomain: usable labels
// in configured order first, then labels in cooldown (soonest to recover first)
// so something is always tried even when every label looks blocked.
func (p *Pool) Hosts(baseDomain string, dc int) []string {
	labels := telegram.CFLabelsFor(dc)
	if p == nil {
		out := make([]string, len(labels))
		for i, l := range labels {
			out[i] = l + "." + baseDomain
		}
		return out
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	type cooling struct {
		host  string
		until time.Time
	}
	var ready []string
	var waiting []cooling
	for _, l := range labels {
		host := l + "." + baseDomain
		if st := p.hosts[host]; st != nil && now.Before(st.until) {
			waiting = append(waiting, cooling{host, st.until})
			continue
		}
		ready = append(ready, host)
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].until.Before(waiting[j].until) })
	for _, w := range waiting {
		ready = append(ready, w.host)
	}
	return ready
}

// Report records the outcome of a connection to host. Only call it for real
// connection failures (not for a caller that gave up); nil clears the cooldown.
// It returns the cooldown applied (0 on success).
func (p *Pool) Report(host string, err error) time.Duration {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.hosts[host]
	if err == nil {
		if st != nil {
			delete(p.hosts, host)
		}
		return 0
	}
	if st == nil {
		st = &state{}
		p.hosts[host] = st
	}
	st.failures++
	n := st.failures - 1
	if n > maxDoublings {
		n = maxDoublings
	}
	d := p.initial << uint(n)
	if d > p.max || d <= 0 {
		d = p.max
	}
	st.until = p.now().Add(d)
	return d
}

// Cooling reports how many hosts are currently in cooldown (for logging).
func (p *Pool) Cooling() int {
	if p == nil {
		return 0
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, st := range p.hosts {
		if now.Before(st.until) {
			n++
		}
	}
	return n
}
