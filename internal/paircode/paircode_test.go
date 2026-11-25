package paircode

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestNewSecretShape(t *testing.T) {
	a, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != SecretLen || len(b) != SecretLen {
		t.Fatalf("secrets %q %q", a, b)
	}
	if a == b {
		t.Fatal("two secrets were identical")
	}
	if !allAlphabet(a) || !allAlphabet(b) {
		t.Fatalf("secret outside alphabet: %q %q", a, b)
	}
}

func TestDeriveIsDeterministicAndSplit(t *testing.T) {
	m, err := Derive("ABCDEFGHJKMN")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Derive("abcdefghjkmn")
	if err != nil {
		t.Fatal(err)
	}
	if m != again {
		t.Fatal("derivation is case-sensitive or nondeterministic")
	}
	if m.ServerPrivate == m.ClientPrivate {
		t.Fatal("server and client keys are identical")
	}
	if m.PresharedKey == ([32]byte{}) {
		t.Fatal("zero pre-shared key")
	}
	if got := hex.EncodeToString(m.ServerPrivate[:]); got == strings.Repeat("00", 32) {
		t.Fatal("zero server key")
	}
}

func TestCodeRoundTripAndResidue(t *testing.T) {
	secret := "ABCDEFGHJKMN"
	for _, region := range []int{1, 2, 31, 32, 33, 65535} {
		code := Code(secret, region)
		p, err := Parse(Format(code))
		if err != nil {
			t.Fatalf("region %d: %v", region, err)
		}
		if p.Secret != secret {
			t.Fatalf("region %d: secret %q", region, p.Secret)
		}
		want := region % len(Alphabet)
		if p.Residue != want {
			t.Fatalf("region %d: residue %d want %d", region, p.Residue, want)
		}
		if len(p.Code) != CodeLen {
			t.Fatalf("region %d: code len %d", region, len(p.Code))
		}
	}
}

func TestParseForgivingAndStrict(t *testing.T) {
	code := Code("P95MAN26ABCD", 1) // P95-MAN-26A-BCD-? display varies; parser is the contract
	for _, in := range []string{
		code,
		strings.ToLower(code),
		Format(code),
		"  " + Format(code) + "\n",
		strings.ReplaceAll(Format(code), "-", " "),
		"\u200b" + Format(code) + "\ufeff",
	} {
		p, err := Parse(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if p.Code != code {
			t.Fatalf("%q: got %q want %q", in, p.Code, code)
		}
	}
	for _, bad := range []string{
		"",
		"P95-MAN-26",
		"P95-MAN-26ABC",
		"P95-MAN-26ABCDEF",
		"ABCDEFGHJKLM!",
		"P95_MAN_26ABCD",
		"ABCDEFGHJKLM0", // 0 is not in the alphabet
		"ABCDEFGHJKLI",  // I is not in the alphabet
	} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("%q parsed", bad)
		}
	}
}

func TestLinksAndRedaction(t *testing.T) {
	code := Code("P95MAN26ABCD", 7)
	link := Link(code)
	if link != "tailconnect://pair/"+Format(code) {
		t.Fatalf("link %q", link)
	}
	for _, in := range []string{link, strings.ToUpper(link), link + "/"} {
		p, err := ParseLink(in)
		if err != nil || p.Code != code {
			t.Fatalf("%q: %+v %v", in, p, err)
		}
	}
	if _, err := ParseLink("tailconnect://pair/"); err == nil {
		t.Fatal("empty link parsed")
	}
	in := "code " + Format(code) + " and tcAbCdEfGhIjKlMnOpQrStUvWxYz012345"
	out := Redact(in)
	if strings.Contains(out, code) || strings.Contains(out, "tcAbCd") {
		t.Fatalf("not redacted: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("no marker: %q", out)
	}
}

func TestPinnedDerivation(t *testing.T) {
	m, err := Derive("ABCDEFGHJKMN")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("server=%x psk=%x client=%x", m.ServerPrivate, m.PresharedKey, m.ClientPrivate)
}
