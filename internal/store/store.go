// Package store loads cru.org redirect and upstream rules from Redis and keeps
// the last good copy in memory.
package store

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultUpstream is the upstream used when no upstream rule matches.
const DefaultUpstream = "DEFAULT_PROXY_TARGET"

// Rule is one regex rule. Value is a replacement template for rewrites and an
// upstream name for upstream rules.
type Rule struct {
	Pattern string
	Re      *regexp.Regexp
	Value   string
}

// Snapshot is an immutable copy of all rules, swapped in whole on each load.
type Snapshot struct {
	Vanities     map[string]string
	Rewrites     []Rule
	Upstreams    []Rule
	ForwardQuery map[string]bool
	LoadedAt     time.Time
}

// Keys names the Redis hashes to read. ForwardQuery is optional.
type Keys struct {
	Vanities     string
	Rewrites     string
	Upstreams    string
	ForwardQuery string
}

// Source reads one Redis hash.
type Source interface {
	HGetAll(ctx context.Context, key string) (map[string]string, error)
}

var groupRef = regexp.MustCompile(`\$(\d+)`)

// Build compiles raw hash contents into a Snapshot. Patterns that fail to
// compile are skipped and reported through bad.
func Build(vanities, rewrites, upstreams, forward map[string]string, bad func(pattern string, err error)) *Snapshot {
	s := &Snapshot{
		Vanities:     vanities,
		Rewrites:     compile(rewrites, true, bad),
		Upstreams:    compile(upstreams, false, bad),
		ForwardQuery: make(map[string]bool, len(forward)),
		LoadedAt:     time.Now(),
	}
	for k := range forward {
		s.ForwardQuery[k] = true
	}
	return s
}

// compile orders patterns longest first, then lexically, so overlapping
// patterns resolve the same way on every task.
func compile(raw map[string]string, template bool, bad func(string, error)) []Rule {
	rules := make([]Rule, 0, len(raw))
	for pattern, value := range raw {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			if bad != nil {
				bad(pattern, err)
			}
			continue
		}
		if template {
			// Go reads "$1x" as a group named "1x"; PCRE reads it as group 1.
			value = groupRef.ReplaceAllString(value, "$${$1}")
		}
		rules = append(rules, Rule{Pattern: pattern, Re: re, Value: value})
	}
	sort.Slice(rules, func(i, j int) bool {
		if len(rules[i].Pattern) != len(rules[j].Pattern) {
			return len(rules[i].Pattern) > len(rules[j].Pattern)
		}
		return rules[i].Pattern < rules[j].Pattern
	})
	return rules
}

// Redirect returns the redirect target for a path and the key that produced
// it (the vanity path or the regex pattern). path is the normalized path in
// its original case; lower is the same path lowercased.
func (s *Snapshot) Redirect(path, lower string) (target, key string, ok bool) {
	if t, found := s.Vanities[lower]; found {
		return t, lower, true
	}
	for _, r := range s.Rewrites {
		if r.Re.MatchString(path) {
			return r.Re.ReplaceAllString(path, r.Value), r.Pattern, true
		}
	}
	return "", "", false
}

// Upstream returns the upstream name for a lowercased path.
func (s *Snapshot) Upstream(lower string) string {
	for _, r := range s.Upstreams {
		if r.Re.MatchString(lower) {
			return r.Value
		}
	}
	return DefaultUpstream
}

// Store holds the current Snapshot and reloads it from a Source.
type Store struct {
	src       Source
	keys      Keys
	minReload time.Duration
	bad       func(string, error)

	snap     atomic.Pointer[Snapshot]
	mu       sync.Mutex
	lastLoad time.Time
}

// New returns a Store. minReload limits how often Reload hits Redis.
func New(src Source, keys Keys, minReload time.Duration, bad func(string, error)) *Store {
	return &Store{src: src, keys: keys, minReload: minReload, bad: bad}
}

// Get returns the current Snapshot, or nil before the first successful load.
func (s *Store) Get() *Snapshot {
	return s.snap.Load()
}

// Load reads every hash and swaps in a new Snapshot. On error the previous
// Snapshot stays in place.
func (s *Store) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(ctx)
}

func (s *Store) load(ctx context.Context) error {
	s.lastLoad = time.Now()
	// An empty required hash is treated as a failed load so a flushed Redis
	// can't wipe the rules a running task already has.
	read := func(key string, required bool) (map[string]string, error) {
		if key == "" {
			return map[string]string{}, nil
		}
		m, err := s.src.HGetAll(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("hgetall %s: %w", key, err)
		}
		if required && len(m) == 0 {
			return nil, fmt.Errorf("hash %s is empty", key)
		}
		return m, nil
	}
	v, err := read(s.keys.Vanities, true)
	if err != nil {
		return err
	}
	rw, err := read(s.keys.Rewrites, true)
	if err != nil {
		return err
	}
	up, err := read(s.keys.Upstreams, true)
	if err != nil {
		return err
	}
	fq, err := read(s.keys.ForwardQuery, false)
	if err != nil {
		return err
	}
	s.snap.Store(Build(v, rw, up, fq, s.bad))
	return nil
}

// Reload loads now unless a load happened within minReload. It backs the
// purge_vanity and purge_target query params.
func (s *Store) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastLoad) < s.minReload {
		return nil
	}
	return s.load(ctx)
}
