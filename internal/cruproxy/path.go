package cruproxy

import (
	"net/url"
	"path"
	"strings"
)

var purgeParams = []string{"purge_vanity", "purge_target"}

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
// the target win, and purge params are never forwarded.
func MergeQuery(target, incoming string) string {
	in, err := url.ParseQuery(incoming)
	if err != nil || len(in) == 0 {
		return target
	}
	for _, p := range purgeParams {
		in.Del(p)
	}

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
