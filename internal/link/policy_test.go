package link

import (
	"net/netip"
	"testing"
)

func TestPolicyParsesAndAllowsPrivateLAN(t *testing.T) {
	p, err := ParsePolicy([]string{"192.168.1.0/24", "10.20.30.40", "172.16.0.0/12"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"192.168.1.1:80", "192.168.1.255:443", "10.20.30.40:22", "172.20.1.2:53"} {
		if !p.Allowed(netip.MustParseAddrPort(s)) {
			t.Errorf("denied %s", s)
		}
	}
	for _, s := range []string{"192.168.2.1:80", "10.20.30.41:22", "8.8.8.8:53", "127.0.0.1:80", "169.254.1.1:80", "224.0.0.1:80"} {
		if p.Allowed(netip.MustParseAddrPort(s)) {
			t.Errorf("allowed %s", s)
		}
	}
}

func TestPolicyRejectsPublicAndUnsafePrefixes(t *testing.T) {
	for _, in := range []string{"0.0.0.0/0", "8.8.8.0/24", "100.64.0.0/10", "127.0.0.1/32"} {
		if _, err := ParsePolicy([]string{in}, false); err == nil {
			t.Errorf("%s accepted", in)
		}
	}
}

func TestPolicyLoopbackOptIn(t *testing.T) {
	p, err := ParsePolicy(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Allowed(netip.MustParseAddrPort("127.0.0.1:8080")) {
		t.Fatal("loopback denied after opt-in")
	}
	if p.Allowed(netip.MustParseAddrPort("192.168.1.1:80")) {
		t.Fatal("unlisted LAN allowed")
	}
}
