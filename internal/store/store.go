// Package store loads cru.org redirect and upstream rules from a JSON document
// and keeps the last good copy in memory.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultUpstream is the upstream used when no upstream rule matches.
const DefaultUpstream = "DEFAULT_PROXY_TARGET"

// ErrNotModified is returned by a Source when the document still has the
// version tag the caller already holds.
var ErrNotModified = errors.New("rules not modified")

// Document is the rules file written by cru-terraform.
type Document struct {
	Vanities     map[string]string `json:"vanities"`
	Rewrites     map[string]string `json:"rewrites"`
	Upstreams    map[string]string `json:"upstreams"`
	ForwardQuery []string          `json:"forward_query"`
}

// Source fetches the rules document. When etag matches the current version it
// returns ErrNotModified instead of the body.
type Source interface {
	Fetch(ctx context.Context, etag string) (body []byte, newETag string, err error)
}

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

var groupRef = regexp.MustCompile(`\$(\d+)`)

// Build compiles a document into a Snapshot. Patterns that fail to compile are
// skipped and reported through bad.
func Build(doc Document, bad func(pattern string, err error)) *Snapshot {
	s := &Snapshot{
		Vanities:     doc.Vanities,
		Rewrites:     compile(doc.Rewrites, true, bad),
		Upstreams:    compile(doc.Upstreams, false, bad),
		ForwardQuery: make(map[string]bool, len(doc.ForwardQuery)),
		LoadedAt:     time.Now(),
	}
	if s.Vanities == nil {
		s.Vanities = map[string]string{}
	}
	for _, k := range doc.ForwardQuery {
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
func (s *Snapshot) Redirect(path, lower string) (string, string, bool) {
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

// Parse decodes and checks a rules document. The vanity, rewrite and upstream
// maps must all be non-empty, so a truncated or emptied file can't wipe the
// rules a running task already has.
func Parse(body []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return doc, fmt.Errorf("decode rules: %w", err)
	}
	switch {
	case len(doc.Vanities) == 0:
		return doc, errors.New("rules document has no vanities")
	case len(doc.Rewrites) == 0:
		return doc, errors.New("rules document has no rewrites")
	case len(doc.Upstreams) == 0:
		return doc, errors.New("rules document has no upstreams")
	}
	return doc, nil
}

// Store holds the current Snapshot and reloads it from a Source.
type Store struct {
	src       Source
	minReload time.Duration
	bad       func(string, error)

	snap     atomic.Pointer[Snapshot]
	mu       sync.Mutex
	etag     string
	lastLoad time.Time
}

// New returns a Store. minReload limits how often Reload hits the Source.
func New(src Source, minReload time.Duration, bad func(string, error)) *Store {
	return &Store{src: src, minReload: minReload, bad: bad}
}

// Get returns the current Snapshot, or nil before the first successful load.
func (s *Store) Get() *Snapshot {
	return s.snap.Load()
}

// Load fetches the document and swaps in a new Snapshot when it changed. On
// error the previous Snapshot stays in place. It reports whether the rules
// changed.
func (s *Store) Load(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(ctx)
}

func (s *Store) load(ctx context.Context) (bool, error) {
	s.lastLoad = time.Now()
	etag := s.etag
	if s.snap.Load() == nil {
		etag = ""
	}
	body, newETag, err := s.src.Fetch(ctx, etag)
	if errors.Is(err, ErrNotModified) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	doc, err := Parse(body)
	if err != nil {
		return false, err
	}
	s.snap.Store(Build(doc, s.bad))
	s.etag = newETag
	return true, nil
}

// Reload loads now unless a load happened within minReload. It backs the
// purge_vanity and purge_target query params.
func (s *Store) Reload(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastLoad) < s.minReload {
		return false, nil
	}
	return s.load(ctx)
}
