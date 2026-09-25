package proxy

import (
	"net/url"
	"path"
	"strings"
)

// NormalizePath merges slashes and resolves dot segments the way nginx does
// before matching, keeping a trailing slash.
func NormalizePath(p string) string {
	if p == "" {
		return "/"
	}
	c := path.Clean(p)
	if strings.HasSuffix(p, "/") && c != "/" {
		c += "/"
	}
	return c
}

// MergeQuery adds the incoming query to a redirect target. Params already in
// the target win, and purge params are never forwarded. A malformed pair is
// dropped on its own; the pairs that parse still carry over.
func MergeQuery(target, incoming string) string {
	in, _ := url.ParseQuery(incoming)
	if len(in) == 0 {
		return target
	}
	in.Del("purge_vanity")
	in.Del("purge_target")

	frag := ""
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target, frag = target[:i], target[i:]
	}
	base, tq, _ := strings.Cut(target, "?")
	have, _ := url.ParseQuery(tq)

	add := url.Values{}
	for k, vs := range in {
		if _, ok := have[k]; !ok {
			add[k] = vs
		}
	}
	if len(add) == 0 {
		return target + frag
	}
	q := tq
	if q != "" {
		q += "&"
	}
	return base + "?" + q + add.Encode() + frag
}
