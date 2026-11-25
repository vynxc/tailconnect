package link

import (
	"fmt"
	"net/netip"
	"strings"
)

// Policy decides which destinations the phone-side server will dial. It is an
// allowlist of private IPv4 prefixes, not a route table: tailconnect never
// installs routes and never forwards general internet traffic.
type Policy struct {
	Prefixes      []netip.Prefix
	AllowLoopback bool
}

func ParsePolicy(values []string, allowLoopback bool) (Policy, error) {
	p := Policy{AllowLoopback: allowLoopback}
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			pfx, err := parsePrefix(part)
			if err != nil {
				return Policy{}, err
			}
			if pfx.Addr().IsLoopback() {
				if !allowLoopback {
					return Policy{}, fmt.Errorf("link: %s is loopback; add --allow-loopback to opt in", pfx)
				}
			} else if !pfx.Addr().IsPrivate() {
				return Policy{}, fmt.Errorf("link: %s is not a private LAN prefix", pfx)
			}
			p.Prefixes = append(p.Prefixes, pfx)
		}
	}
	if allowLoopback {
		p.Prefixes = append(p.Prefixes, netip.MustParsePrefix("127.0.0.0/8"))
	}
	return p, nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("link: parse prefix %q: %w", s, err)
		}
		p = p.Masked()
		a := p.Addr()
		bits := p.Bits()
		if a.Is6() {
			a = a.Unmap()
			bits -= 96
		}
		if !a.Is4() || bits < 0 || bits > 32 {
			return netip.Prefix{}, fmt.Errorf("link: only IPv4 LAN prefixes are supported, got %s", p)
		}
		return netip.PrefixFrom(a, bits), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("link: parse address %q: %w", s, err)
	}
	a = a.Unmap()
	if !a.Is4() {
		return netip.Prefix{}, fmt.Errorf("link: only IPv4 LAN addresses are supported, got %s", a)
	}
	return netip.PrefixFrom(a, 32), nil
}

var alwaysBlocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
}

// Allowed reports whether dst may be dialed from the phone.
func (p Policy) Allowed(dst netip.AddrPort) bool {
	a := dst.Addr().Unmap()
	if !a.IsValid() || !a.Is4() || dst.Port() == 0 {
		return false
	}
	for _, blocked := range alwaysBlocked {
		if blocked.Contains(a) {
			return false
		}
	}
	if a.IsLoopback() && !p.AllowLoopback {
		return false
	}
	for _, pfx := range p.Prefixes {
		if pfx.Contains(a) {
			return true
		}
	}
	return false
}

func (p Policy) String() string {
	if len(p.Prefixes) == 0 {
		return "no destinations allowed"
	}
	parts := make([]string, 0, len(p.Prefixes))
	for _, pfx := range p.Prefixes {
		parts = append(parts, pfx.String())
	}
	return strings.Join(parts, ",")
}
