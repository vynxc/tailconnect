package link

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/tailscale/tailcat"
	"github.com/xtaci/smux"

	"github.com/vynxc/tailconnect/internal/protocol"
)

func TestReverseProxyOverSmux(t *testing.T) {
	backend, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		for {
			c, err := backend.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4)
				if _, err := io.ReadFull(c, buf); err != nil {
					return
				}
				c.Write([]byte("pong"))
			}(c)
		}
	}()

	serverConn, clientConn := net.Pipe()
	serverSess, err := smux.Server(serverConn, smux.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	clientSess, err := smux.Client(clientConn, smux.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer clientSess.Close()

	go func() {
		for {
			stream, err := clientSess.AcceptStream()
			if err != nil {
				return
			}
			go func(stream *smux.Stream) {
				defer stream.Close()
				h, err := protocol.ReadHeader(stream)
				if err != nil || h.Service != "web" {
					return
				}
				remote, err := net.Dial("tcp4", backend.Addr().String())
				if err != nil {
					return
				}
				tailcat.ProxyConns(stream, remote)
			}(stream)
		}
	}()

	rs, err := newReverseSession(&Server{}, serverSess, protocol.Manifest{
		Version:  protocol.Version,
		Agent:    "test",
		Services: []protocol.Service{{ID: "web", Label: "Web"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rs.close()

	if len(rs.services) != 1 {
		t.Fatalf("services: %+v", rs.services)
	}
	conn, err := net.DialTimeout("tcp4", "127.0.0.1:"+strconv.Itoa(rs.services[0].Port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "pong" {
		t.Fatalf("got %q", buf)
	}
}
