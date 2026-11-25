// Package paircode implements tailconnect's short pairing code.
//
// A code is thirteen characters from Alphabet: twelve characters of secret
// followed by one character naming the DERP region residue. The secret is a
// password, not an identifier: both sides derive the same three keys from it
// with Argon2id, so no directory or coordination server is needed.
//
// Alphabet has 30 characters, so a twelve-character secret carries about 58.9
// bits of entropy.
package paircode

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// Alphabet deliberately omits 0, 1, I, L, O and U so a code read aloud
	// or typed from a screen is less ambiguous.
	Alphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

	SecretLen = 12
	CodeLen   = SecretLen + 1

	// KDF parameters. These are intentionally identical in shape to the
	// Argon2id work Hideout uses for its game invite codes, with a
	// tailconnect-specific salt and enough output for an independent client
	// identity.
	kdfSalt    = "tailconnect pair v1"
	kdfTime    = 3
	kdfMemory  = 64 * 1024 // KiB
	kdfThreads = 1
	kdfBytes   = 96
)

var (
	ErrBadSecret = errors.New("paircode: invalid secret")
	ErrBadCode   = errors.New("paircode: invalid pairing code")
)

// Material is the key material both sides derive from a code secret.
//
// The server key authenticates the Android side. The client key authenticates
// the agent side, and the server allowlists it, so leaking the server's
// tailcat address alone is not enough to connect. The pre-shared key is mixed
// into WireGuard and is carried inside the tailcat address.
type Material struct {
	ServerPrivate [32]byte
	ClientPrivate [32]byte
	PresharedKey  [32]byte
	Secret        string
}

// Parsed is a normalized code.
type Parsed struct {
	Code    string // bare, 13 characters
	Secret  string // first 12 characters
	Residue int    // RegionID % len(Alphabet), from the 13th character
}

// NewSecret returns twelve random characters from Alphabet.
func NewSecret() (string, error) {
	out := make([]byte, 0, SecretLen)
	var b [1]byte
	// Reject the high tail so every character is uniformly likely. 256 is
	// not divisible by the 30-character alphabet.
	const unbiasedBelow = 256 - 256%len(Alphabet)
	for len(out) < SecretLen {
		if _, err := rand.Read(b[:]); err != nil {
			return "", fmt.Errorf("paircode: random secret: %w", err)
		}
		if int(b[0]) >= unbiasedBelow {
			continue
		}
		out = append(out, Alphabet[int(b[0])%len(Alphabet)])
	}
	return string(out), nil
}

// Derive expands a secret into the three keys used by a pair session.
func Derive(secret string) (Material, error) {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	if len(secret) != SecretLen || !allAlphabet(secret) {
		return Material{}, ErrBadSecret
	}
	raw := argon2.IDKey([]byte(secret), []byte(kdfSalt), kdfTime, kdfMemory, kdfThreads, kdfBytes)
	var m Material
	copy(m.ServerPrivate[:], raw[:32])
	copy(m.PresharedKey[:], raw[32:64])
	copy(m.ClientPrivate[:], raw[64:96])
	m.Secret = secret
	return m, nil
}

// Code appends the region residue to a secret.
func Code(secret string, regionID int) string {
	residue := regionID % len(Alphabet)
	if residue < 0 {
		residue += len(Alphabet)
	}
	return strings.ToUpper(secret) + string(Alphabet[residue])
}

// Format groups a bare code as XXXX-XXXX-XXXXX for display.
func Format(code string) string {
	code = Normalize(code)
	if len(code) != CodeLen {
		return code
	}
	return code[:4] + "-" + code[4:8] + "-" + code[8:]
}

// Normalize strips presentation characters, zero-width characters and case.
func Normalize(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch r {
		case '-', ' ', '\t', '\r', '\n', '\u00ad', '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			continue
		}
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Parse reads a bare code in any case, with or without dashes and whitespace.
func Parse(text string) (Parsed, error) {
	code := Normalize(text)
	if len(code) != CodeLen {
		return Parsed{}, ErrBadCode
	}
	secret := code[:SecretLen]
	if !allAlphabet(secret) {
		return Parsed{}, ErrBadCode
	}
	residue := strings.IndexByte(Alphabet, code[SecretLen])
	if residue < 0 {
		return Parsed{}, ErrBadCode
	}
	return Parsed{Code: code, Secret: secret, Residue: residue}, nil
}

// ParseLink accepts a bare code or a tailconnect://pair/<code> link.
func ParseLink(text string) (Parsed, error) {
	t := strings.TrimSpace(text)
	lower := strings.ToLower(t)
	for _, prefix := range []string{"tailconnect://pair/", "tailconnect:pair/"} {
		if strings.HasPrefix(lower, prefix) {
			rest := strings.TrimSpace(t[len(prefix):])
			rest = strings.TrimSuffix(rest, "/")
			return Parse(rest)
		}
	}
	return Parse(t)
}

// Link returns the app link for a bare code.
func Link(code string) string {
	return "tailconnect://pair/" + Format(code)
}

var redactRE = regexp.MustCompile(`(?i)\b[2-9A-HJKMNP-TV-Z]{4}[- ]?[2-9A-HJKMNP-TV-Z]{4}[- ]?[2-9A-HJKMNP-TV-Z]{5}\b|tc[A-Za-z0-9_-]{16,}`)

// Redact removes anything shaped like a pairing code or a tailcat address
// from text intended for logs.
func Redact(text string) string {
	return redactRE.ReplaceAllString(text, "[redacted]")
}

func allAlphabet(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune(Alphabet, r) {
			return false
		}
	}
	return true
}
