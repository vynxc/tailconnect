// Package protocol is the small control protocol spoken over the tailcat
// tunnel between the Android server and the agent client.
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	Version            = 1
	DefaultControlPort = 7000
	MaxMessage         = 64 << 10
)

var (
	ErrMessageTooLarge = errors.New("protocol: message too large")
	ErrBadVersion      = errors.New("protocol: unsupported version")
)

// Service is an agent-side endpoint the phone may open. Target deliberately
// stays on the agent: the phone only learns the ID, label and preferred local
// port, never the agent's private address.
type Service struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Protocol      string `json:"protocol,omitempty"` // "tcp" (default)
	PreferredPort int    `json:"preferredPort,omitempty"`
}

// Manifest is sent once when an agent connects.
type Manifest struct {
	Version  int       `json:"v"`
	Agent    string    `json:"agent,omitempty"`
	Services []Service `json:"services"`
}

// StreamHeader prefixes an agent-bound stream opened by the phone.
type StreamHeader struct {
	Version int    `json:"v"`
	Service string `json:"service"`
}

func WriteMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxMessage {
		return ErrMessageTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func ReadMessage(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > MaxMessage {
		return ErrMessageTooLarge
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("protocol: decode: %w", err)
	}
	return nil
}

func WriteHeader(w io.Writer, h StreamHeader) error {
	return WriteMessage(w, h)
}

func ReadHeader(r io.Reader) (StreamHeader, error) {
	var h StreamHeader
	if err := ReadMessage(r, &h); err != nil {
		return StreamHeader{}, err
	}
	if h.Version != Version {
		return StreamHeader{}, ErrBadVersion
	}
	if h.Service == "" {
		return StreamHeader{}, errors.New("protocol: empty service")
	}
	return h, nil
}
