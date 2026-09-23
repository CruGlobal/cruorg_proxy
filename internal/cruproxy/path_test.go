package cruproxy_test

import (
	"testing"

	"github.com/CruGlobal/cruorg_proxy/internal/cruproxy"
)

func TestNormalizePath(t *testing.T) {
	for in, want := range map[string]string{
		"":           "/",
		"/":          "/",
		"//10steps":  "/10steps",
		"/a//b/../c": "/a/c",
		"/campus/":   "/campus/",
		"/x/./y/":    "/x/y/",
	} {
		if got := cruproxy.NormalizePath(in); got != want {
			t.Errorf("cruproxy.NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeQuery(t *testing.T) {
	cases := []struct{ target, in, want string }{
		{"/about.html", "", "/about.html"},
		{"/about.html", "utm_source=a", "/about.html?utm_source=a"},
		{"https://x.com/e/?e=31709", "e=1&utm_source=a", "https://x.com/e/?e=31709&utm_source=a"},
		{"/a", "purge_vanity=1", "/a"},
		{"/a#top", "b=2&purge_target", "/a?b=2#top"},
	}
	for _, c := range cases {
		if got := cruproxy.MergeQuery(c.target, c.in); got != c.want {
			t.Errorf("cruproxy.MergeQuery(%q, %q) = %q, want %q", c.target, c.in, got, c.want)
		}
	}
}
