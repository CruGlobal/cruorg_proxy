package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

func TestRedirectVanityBeforeRegex(t *testing.T) {
	s := store.Build(
		map[string]string{"/campus/x": "/vanity"},
		map[string]string{"^/campus/(.*)": "/communities/campus/$1"},
		nil, nil, nil)
	if got, key, _ := s.Redirect("/campus/x", "/campus/x"); got != "/vanity" || key != "/campus/x" {
		t.Fatalf("got %q key %q", got, key)
	}
	if got, key, _ := s.Redirect("/Campus/Y", "/campus/y"); got != "/communities/campus/Y" || key != "^/campus/(.*)" {
		t.Fatalf("got %q key %q", got, key)
	}
}

func TestVanityKeysAreExact(t *testing.T) {
	s := store.Build(map[string]string{"/whoisJesus": "/x"}, nil, nil, nil, nil)
	if _, _, ok := s.Redirect("/whoisJesus", "/whoisjesus"); ok {
		t.Fatal("mixed-case vanity key matched; nginx never matches these")
	}
}

func TestRewriteOrderLongestFirst(t *testing.T) {
	s := store.Build(nil, map[string]string{
		"^/ministries-and-locations(.*)":                               "/communities/locations$1",
		"^/ministries-and-locations/ministries(.*)":                    "/communities/ministries$1",
		"^/ministries-and-locations/ministries/athletes-in-action(.*)": "/communities/athletes$1",
	}, nil, nil, nil)
	for range 20 {
		got, _, _ := s.Redirect("/ministries-and-locations/ministries/athletes-in-action/x", "")
		if got != "/communities/athletes/x" {
			t.Fatalf("got %q", got)
		}
	}
}

func TestGroupRefFollowedByText(t *testing.T) {
	s := store.Build(nil, map[string]string{"^(.*).htm$": "$1.html", "^/a/(.*)": "/b/$1x"}, nil, nil, nil)
	if got, _, _ := s.Redirect("/foo/bar.htm", ""); got != "/foo/bar.html" {
		t.Fatalf("got %q", got)
	}
	if got, _, _ := s.Redirect("/a/z", ""); got != "/b/zx" {
		t.Fatalf("got %q", got)
	}
}

func TestUpstream(t *testing.T) {
	s := store.Build(nil, nil, map[string]string{"^/wp-admin": "VIP_ADDR", "^.*\\.php.*": "VIP_ADDR"}, nil, nil)
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
		nil,
		map[string]string{"^/(a": "/x", "^/b": "/y"},
		nil,
		nil,
		func(p string, _ error) { bad = append(bad, p) },
	)
	if len(s.Rewrites) != 1 || len(bad) != 1 {
		t.Fatalf("rewrites %d bad %v", len(s.Rewrites), bad)
	}
}

type fakeSource struct {
	data  map[string]map[string]string
	err   error
	calls int
}

func (f *fakeSource) HGetAll(_ context.Context, key string) (map[string]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.data[key], nil
}

func TestLoadKeepsLastGoodCopy(t *testing.T) {
	src := &fakeSource{data: map[string]map[string]string{"v": {"/a": "/b"}}}
	st := store.New(src, store.Keys{Vanities: "v"}, 0, nil)
	if err := st.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	src.err = errors.New("down")
	if err := st.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "down") {
		t.Fatalf("err %v", err)
	}
	if st.Get() == nil || st.Get().Vanities["/a"] != "/b" {
		t.Fatal("lost last good copy")
	}
}

func TestEmptyRequiredHashFails(t *testing.T) {
	src := &fakeSource{data: map[string]map[string]string{"v": {"/a": "/b"}}}
	st := store.New(src, store.Keys{Vanities: "v"}, 0, nil)
	_ = st.Load(context.Background())
	src.data["v"] = map[string]string{}
	if err := st.Load(context.Background()); err == nil {
		t.Fatal("empty hash loaded")
	}
	if st.Get().Vanities["/a"] != "/b" {
		t.Fatal("lost last good copy")
	}
}

func TestReloadRateLimited(t *testing.T) {
	src := &fakeSource{data: map[string]map[string]string{"v": {"/a": "/b"}}}
	st := store.New(src, store.Keys{Vanities: "v"}, time.Hour, nil)
	_ = st.Load(context.Background())
	before := src.calls
	_ = st.Reload(context.Background())
	if src.calls != before {
		t.Fatal("reload inside min interval hit redis")
	}
}
