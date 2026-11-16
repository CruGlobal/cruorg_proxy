package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CruGlobal/cruorg_proxy/internal/proxy"
)

func TestClientIP(t *testing.T) {
	trusted, err := proxy.ParseTrusted("10.16.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, peer, xff, want string
	}{
		{"untrusted peer ignores header", "198.51.100.7:1", "1.2.3.4", "198.51.100.7"},
		{"ALB peer, CloudFront hop", "10.16.3.4:1", "203.0.113.9, 130.176.1.1", "130.176.1.1"},
		{"ALB peer, spoofed left end", "10.16.3.4:1", "6.6.6.6, 203.0.113.9", "203.0.113.9"},
		{"ALB peer, chain of trusted hops", "10.16.3.4:1", "203.0.113.9, 10.16.9.9", "203.0.113.9"},
		{"ALB peer, no header", "10.16.3.4:1", "", "10.16.3.4"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.peer
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := trusted.ClientIP(r); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestViewerIP(t *testing.T) {
	trusted, err := proxy.ParseTrusted("10.16.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	const cfHop = "203.0.113.9, 130.176.1.1"
	cases := []struct {
		name, peer, viewer, want string
	}{
		{"ALB peer, IPv4 viewer", "10.16.3.4:1", "198.51.100.5:443", "198.51.100.5"},
		{"ALB peer, IPv6 viewer", "10.16.3.4:1", "2001:db8::5:443", "2001:db8::5"},
		{"ALB peer, bracketed IPv6", "10.16.3.4:1", "[2001:db8::5]:443", "2001:db8::5"},
		{"ALB peer, no viewer header", "10.16.3.4:1", "", "130.176.1.1"},
		{"ALB peer, garbage viewer header", "10.16.3.4:1", "nope", "130.176.1.1"},
		{"untrusted peer ignores viewer header", "198.51.100.7:1", "6.6.6.6:1", "198.51.100.7"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.peer
		r.Header.Set("X-Forwarded-For", cfHop)
		if c.viewer != "" {
			r.Header.Set("CloudFront-Viewer-Address", c.viewer)
		}
		if got := trusted.ViewerIP(r); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseTrusted(t *testing.T) {
	if _, err := proxy.ParseTrusted("10.16.0.0/16, private_ranges"); err != nil {
		t.Fatal(err)
	}
	if _, err := proxy.ParseTrusted("not-a-cidr"); err == nil {
		t.Fatal("bad CIDR accepted")
	}
}
