package protocol

import (
	"bytes"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Manifest{
		Version:  Version,
		Agent:    "workstation",
		Services: []Service{{ID: "web", Label: "Dev", Protocol: "tcp", PreferredPort: 3000}},
	}
	if err := WriteMessage(&buf, in); err != nil {
		t.Fatal(err)
	}
	var out Manifest
	if err := ReadMessage(&buf, &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != in.Version || out.Agent != in.Agent || len(out.Services) != 1 || out.Services[0] != in.Services[0] {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestStreamHeaderVersionAndService(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHeader(&buf, StreamHeader{Version: Version, Service: "web"}); err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeader(&buf)
	if err != nil || h.Service != "web" {
		t.Fatalf("%+v %v", h, err)
	}

	buf.Reset()
	_ = WriteMessage(&buf, StreamHeader{Version: 99, Service: "web"})
	if _, err := ReadHeader(&buf); err == nil {
		t.Fatal("wrong version accepted")
	}
}

func TestContextStreamHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHeader(&buf, StreamHeader{Version: Version, Kind: StreamKindContext}); err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeader(&buf)
	if err != nil || h.Kind != StreamKindContext {
		t.Fatalf("%+v %v", h, err)
	}

	buf.Reset()
	_ = WriteMessage(&buf, StreamHeader{Version: Version, Kind: "bogus"})
	if _, err := ReadHeader(&buf); err == nil {
		t.Fatal("unknown stream kind accepted")
	}
}

func TestPhoneContextRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := PhoneContext{
		Version:    ContextVersion,
		Phone:      "Pixel",
		Network:    Network{Type: "wifi", Address: "192.168.4.50", Prefix: "192.168.4.0/24"},
		Services:   []PublishedService{{ID: "web", Label: "Web", Port: 3000, URL: "http://localhost:3000/"}},
		ADB:        []ADBService{{Name: "adb", Type: "_adb-tls-connect._tcp", Host: "192.168.4.50", Port: 5555}},
		Discovered: []DiscoveredService{{Name: "TV", Type: "_googlecast._tcp", Host: "192.168.4.60", Port: 8009}},
	}
	if err := WriteMessage(&buf, in); err != nil {
		t.Fatal(err)
	}
	var out PhoneContext
	if err := ReadMessage(&buf, &out); err != nil {
		t.Fatal(err)
	}
	if out.Phone != in.Phone || out.Network.Prefix != in.Network.Prefix || len(out.Services) != 1 || len(out.ADB) != 1 || len(out.Discovered) != 1 {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestMessageLimit(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, bytes.Repeat([]byte("x"), MaxMessage)); err == nil {
		t.Fatal("oversized message accepted")
	}
}
