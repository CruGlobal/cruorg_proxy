package proxy

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies decides which peers may supply X-Forwarded-For.
type TrustedProxies []netip.Prefix

// ParseTrusted parses a comma-separated list of CIDRs. "private_ranges" expands
// to the RFC 1918, RFC 4193 and loopback ranges.
func ParseTrusted(s string) (TrustedProxies, error) {
	var out TrustedProxies
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		switch part {
		case "":
			continue
		case "private_ranges":
			for _, c := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "fc00::/7", "::1/128"} {
				out = append(out, netip.MustParsePrefix(c))
			}
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (t TrustedProxies) contains(ip netip.Addr) bool {
	for _, p := range t {
		if p.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}

// PeerIP returns the address of the directly connected peer.
func PeerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ViewerIP returns the visitor's IP from CloudFront-Viewer-Address when a
// trusted proxy sent the request, else ClientIP. X-Forwarded-For stops at the
// CloudFront edge because only the ALB is trusted; CloudFront sets this header
// itself, and the ALB only accepts CloudFront.
func (t TrustedProxies) ViewerIP(r *http.Request) string {
	peer, err := netip.ParseAddr(PeerIP(r))
	if err == nil && t.contains(peer) {
		if ip, ok := viewerAddr(r.Header.Get("Cloudfront-Viewer-Address")); ok {
			return ip
		}
	}
	return t.ClientIP(r)
}

// viewerAddr strips the port from CloudFront's "ip:port", whose IPv6 form has
// no brackets.
func viewerAddr(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if ap, err := netip.ParseAddrPort(v); err == nil {
		return ap.Addr().Unmap().String(), true
	}
	if i := strings.LastIndexByte(v, ':'); i > 0 {
		if ip, err := netip.ParseAddr(v[:i]); err == nil {
			return ip.Unmap().String(), true
		}
	}
	return "", false
}

// ClientIP walks X-Forwarded-For right to left from the peer and returns the
// first address that is not a trusted proxy, like nginx's real_ip_recursive.
// A visitor can prepend anything to the header, so the left end is never read
// on trust.
func (t TrustedProxies) ClientIP(r *http.Request) string {
	current := PeerIP(r)
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; ; i-- {
		ip, err := netip.ParseAddr(current)
		if err != nil || !t.contains(ip) {
			return current
		}
		for i >= 0 && strings.TrimSpace(hops[i]) == "" {
			i--
		}
		if i < 0 {
			return current
		}
		current = strings.TrimSpace(hops[i])
	}
}
