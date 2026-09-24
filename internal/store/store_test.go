package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

func doc(v, rw, up map[string]string) store.Document {
	return store.Document{Vanities: v, Rewrites: rw, Upstreams: up}
}

func TestRedirectVanityBeforeRegex(t *testing.T) {
	s := store.Build(doc(
		map[string]string{"/campus/x": "/vanity"},
		map[string]string{"^/campus/(.*)": "/communities/campus/$1"},
		nil), nil)
	if got, key, _ := s.Redirect("/campus/x", "/campus/x"); got != "/vanity" || key != "/campus/x" {
		t.Fatalf("got %q key %q", got, key)
	}
	if got, key, _ := s.Redirect("/Campus/Y", "/campus/y"); got != "/communities/campus/Y" || key != "^/campus/(.*)" {
		t.Fatalf("got %q key %q", got, key)
	}
}

func TestVanityKeysAreExact(t *testing.T) {
	s := store.Build(doc(map[string]string{"/whoisJesus": "/x"}, nil, nil), nil)
	if _, _, ok := s.Redirect("/whoisJesus", "/whoisjesus"); ok {
		t.Fatal("mixed-case vanity key matched; the lookup lowercases the path")
	}
}

func TestRewriteOrderLongestFirst(t *testing.T) {
	s := store.Build(doc(nil, map[string]string{
		"^/ministries-and-locations(.*)":                               "/communities/locations$1",
		"^/ministries-and-locations/ministries(.*)":                    "/communities/ministries$1",
		"^/ministries-and-locations/ministries/athletes-in-action(.*)": "/communities/athletes$1",
	}, nil), nil)
	for range 20 {
		got, _, _ := s.Redirect("/ministries-and-locations/ministries/athletes-in-action/x", "")
		if got != "/communities/athletes/x" {
			t.Fatalf("got %q", got)
		}
	}
}

func TestGroupRefFollowedByText(t *testing.T) {
	s := store.Build(doc(nil, map[string]string{"^(.*).htm$": "$1.html", "^/a/(.*)": "/b/$1x"}, nil), nil)
	if got, _, _ := s.Redirect("/foo/bar.htm", ""); got != "/foo/bar.html" {
		t.Fatalf("got %q", got)
	}
	if got, _, _ := s.Redirect("/a/z", ""); got != "/b/zx" {
		t.Fatalf("got %q", got)
	}
}

func TestUpstream(t *testing.T) {
	s := store.Build(doc(nil, nil, map[string]string{"^/wp-admin": "VIP_ADDR", "^.*\\.php.*": "VIP_ADDR"}), nil)
	if got := s.Upstream("/wp-admin/x"); got != "VIP_ADDR" {
		t.Fatalf("got %q", got)
	}
	if got := s.Upstream("/us/en.html"); got != store.DefaultUpstream {
		t.Fatalf("got %q", got)
	}
}

func TestBadPatternSkipped(t *testing.T) {
	var bad []string
	s := store.Build(
		doc(nil, map[string]string{"^/(a": "/x", "^/b": "/y"}, nil),
		func(p string, _ error) { bad = append(bad, p) },
	)
	if len(s.Rewrites) != 1 || len(bad) != 1 {
		t.Fatalf("rewrites %d bad %v", len(s.Rewrites), bad)
	}
}

func TestParseRejectsEmptySections(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       `{`,
		"no vanities":    `{"rewrites":{"a":"b"},"upstreams":{"a":"b"}}`,
		"no rewrites":    `{"vanities":{"a":"b"},"upstreams":{"a":"b"}}`,
		"no upstreams":   `{"vanities":{"a":"b"},"rewrites":{"a":"b"}}`,
		"empty vanities": `{"vanities":{},"rewrites":{"a":"b"},"upstreams":{"a":"b"}}`,
	} {
		if _, err := store.Parse([]byte(body)); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
}

type fakeSource struct {
	body  []byte
	etag  string
	err   error
	calls int
}

func (f *fakeSource) Fetch(_ context.Context, etag string) ([]byte, string, error) {
	f.calls++
	if f.err != nil {
		return nil, "", f.err
	}
	if etag != "" && etag == f.etag {
		return nil, etag, store.ErrNotModified
	}
	return f.body, f.etag, nil
}

func validBody(t *testing.T, target string) []byte {
	t.Helper()
	b, err := json.Marshal(store.Document{
		Vanities:  map[string]string{"/a": target},
		Rewrites:  map[string]string{"^/r/(.*)": "/x/$1"},
		Upstreams: map[string]string{"^/wp": "VIP_ADDR"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLoadKeepsLastGoodCopy(t *testing.T) {
	src := &fakeSource{body: validBody(t, "/b"), etag: "1"}
	st := store.New(src, 0, nil)
	if changed, err := st.Load(context.Background()); err != nil || !changed {
		t.Fatalf("changed %v err %v", changed, err)
	}
	src.err = errors.New("down")
	if _, err := st.Load(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	src.err = nil
	src.body, src.etag = []byte(`{"vanities":{}}`), "2"
	if _, err := st.Load(context.Background()); err == nil {
		t.Fatal("empty document loaded")
	}
	if st.Get().Vanities["/a"] != "/b" {
		t.Fatal("lost last good copy")
	}
}

func TestLoadNotModified(t *testing.T) {
	src := &fakeSource{body: validBody(t, "/b"), etag: "1"}
	st := store.New(src, 0, nil)
	_, _ = st.Load(context.Background())
	before := st.Get()
	if changed, err := st.Load(context.Background()); err != nil || changed {
		t.Fatalf("changed %v err %v", changed, err)
	}
	if st.Get() != before {
		t.Fatal("snapshot replaced on not-modified")
	}
}

func TestOnLoadSeesPurgeReloads(t *testing.T) {
	src := &fakeSource{body: validBody(t, "/b"), etag: "1"}
	st := store.New(src, 0, nil)
	var calls []bool
	st.OnLoad(func(changed bool, _ error, _ *store.Snapshot) { calls = append(calls, changed) })
	_, _ = st.Load(context.Background())
	src.body, src.etag = validBody(t, "/c"), "2"
	_, _ = st.Reload(context.Background())
	if len(calls) != 2 || !calls[1] {
		t.Fatalf("OnLoad calls %v, want the purge reload reported as changed", calls)
	}
}

func TestReloadRateLimited(t *testing.T) {
	src := &fakeSource{body: validBody(t, "/b"), etag: "1"}
	st := store.New(src, time.Hour, nil)
	_, _ = st.Load(context.Background())
	before := src.calls
	_, _ = st.Reload(context.Background())
	if src.calls != before {
		t.Fatal("reload inside min interval hit the source")
	}
}

func TestFileSource(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(p, validBody(t, "/b"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := store.New(store.FileSource{Path: p}, 0, nil)
	if changed, err := st.Load(context.Background()); err != nil || !changed {
		t.Fatalf("changed %v err %v", changed, err)
	}
	if changed, _ := st.Load(context.Background()); changed {
		t.Fatal("unchanged file reloaded")
	}
	later := time.Now().Add(time.Second)
	if err := os.WriteFile(p, validBody(t, "/c"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(p, later, later)
	if changed, err := st.Load(context.Background()); err != nil || !changed {
		t.Fatalf("changed %v err %v", changed, err)
	}
	if st.Get().Vanities["/a"] != "/c" {
		t.Fatal("new file content not loaded")
	}
}
