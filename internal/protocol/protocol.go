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
	ContextVersion     = 1
	DefaultControlPort = 7000
	MaxMessage         = 64 << 10

	StreamKindService = "service"
	StreamKindContext = "context"
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

// StreamHeader prefixes a stream opened by the phone. Service streams are the
// original kind; context streams carry PhoneContext updates.
type StreamHeader struct {
	Version int    `json:"v"`
	Kind    string `json:"kind,omitempty"`
	Service string `json:"service,omitempty"`
}

// Network describes the phone's active network as seen by Android.
type Network struct {
	Type      string   `json:"type,omitempty"`      // wifi, ethernet, cellular, none
	Interface string   `json:"interface,omitempty"` // wlan0, eth0, ...
	Address   string   `json:"address,omitempty"`   // 192.168.1.50
	Prefix    string   `json:"prefix,omitempty"`    // 192.168.1.0/24
	Gateway   string   `json:"gateway,omitempty"`
	DNS       []string `json:"dns,omitempty"`
}

// PublishedService is a reverse service the phone has made available.
type PublishedService struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Port  int    `json:"port"`
	URL   string `json:"url"`
}

// ADBService is a wireless-debugging endpoint discovered by the phone.
type ADBService struct {
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	Pairing bool   `json:"pairing,omitempty"`
}

// DiscoveredService is an mDNS service discovered on the phone's LAN.
type DiscoveredService struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
}

// PhoneContext is sent by the phone as soon as an agent connects, and again
// whenever the phone's services or discovery results change.
type PhoneContext struct {
	Version       int                 `json:"v"`
	Phone         string              `json:"phone,omitempty"`
	App           string              `json:"app,omitempty"`
	Network       Network             `json:"network"`
	Policy        string              `json:"policy,omitempty"`
	AllowLoopback bool                `json:"allowLoopback,omitempty"`
	ControlPort   int                 `json:"controlPort,omitempty"`
	Region        int                 `json:"region,omitempty"`
	Services      []PublishedService  `json:"services,omitempty"`
	ADB           []ADBService        `json:"adb,omitempty"`
	Discovered    []DiscoveredService `json:"discovered,omitempty"`
}

// ContextInput is a line written to the server's stdin by the Android app.
type ContextInput struct {
	Type string              `json:"type"`
	ADB  []ADBService        `json:"adb,omitempty"`
	MDNS []DiscoveredService `json:"mdns,omitempty"`
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
	switch h.Kind {
	case StreamKindContext:
		return h, nil
	case "", StreamKindService:
		if h.Service == "" {
			return StreamHeader{}, errors.New("protocol: empty service")
		}
		return h, nil
	default:
		return StreamHeader{}, fmt.Errorf("protocol: unknown stream kind %q", h.Kind)
	}
}
