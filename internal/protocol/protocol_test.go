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

func TestMessageLimit(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, bytes.Repeat([]byte("x"), MaxMessage)); err == nil {
		t.Fatal("oversized message accepted")
	}
}
