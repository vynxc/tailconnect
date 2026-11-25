package link

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/tailscale/tailcat"
)

type dialFunc func(context.Context, netip.AddrPort) (net.Conn, error)

type SocksServer struct {
	ln        net.Listener
	dial      dialFunc
	logf      func(string, ...any)
	closeOnce sync.Once
}

func NewSocksServer(listen string, dial dialFunc, logf func(string, ...any)) (*SocksServer, error) {
	if logf == nil {
		logf = log.Printf
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	s := &SocksServer{ln: ln, dial: dial, logf: logf}
	go s.accept()
	return s, nil
}

func (s *SocksServer) Addr() net.Addr { return s.ln.Addr() }

func (s *SocksServer) Close() error {
	var err error
	s.closeOnce.Do(func() { err = s.ln.Close() })
	return err
}

func (s *SocksServer) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *SocksServer) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := socksHandshake(conn); err != nil {
		return
	}
	cmd, ap, err := socksRequest(conn)
	if err != nil {
		return
	}
	if cmd != 1 {
		socksReply(conn, 0x07)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	remote, err := s.dial(ctx, ap)
	cancel()
	if err != nil {
		socksReply(conn, 0x05)
		return
	}
	if err := socksReply(conn, 0x00); err != nil {
		remote.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	tailcat.ProxyConns(conn, remote)
}

func socksHandshake(rw io.ReadWriter) error {
	var hdr [2]byte
	if _, err := io.ReadFull(rw, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != 5 {
		return errors.New("socks: bad version")
	}
	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(rw, methods); err != nil {
		return err
	}
	ok := false
	for _, m := range methods {
		if m == 0 {
			ok = true
			break
		}
	}
	if !ok {
		rw.Write([]byte{5, 0xff})
		return errors.New("socks: no acceptable auth method")
	}
	_, err := rw.Write([]byte{5, 0})
	return err
}

func socksRequest(r io.Reader) (byte, netip.AddrPort, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, netip.AddrPort{}, err
	}
	if hdr[0] != 5 {
		return 0, netip.AddrPort{}, errors.New("socks: bad request version")
	}
	cmd := hdr[1]
	var addr netip.Addr
	switch hdr[3] {
	case 1:
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, netip.AddrPort{}, err
		}
		addr = netip.AddrFrom4(b)
	case 3:
		var n [1]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return 0, netip.AddrPort{}, err
		}
		if n[0] == 0 {
			return 0, netip.AddrPort{}, errors.New("socks: empty domain")
		}
		name := make([]byte, int(n[0]))
		if _, err := io.ReadFull(r, name); err != nil {
			return 0, netip.AddrPort{}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", string(name))
		cancel()
		if err != nil || len(ips) == 0 {
			return 0, netip.AddrPort{}, fmt.Errorf("socks: resolve %q: %w", name, err)
		}
		addr = ips[0]
	case 4:
		var b [16]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, netip.AddrPort{}, err
		}
		addr = netip.AddrFrom16(b)
	default:
		return 0, netip.AddrPort{}, errors.New("socks: bad address type")
	}
	var p [2]byte
	if _, err := io.ReadFull(r, p[:]); err != nil {
		return 0, netip.AddrPort{}, err
	}
	return cmd, netip.AddrPortFrom(addr.Unmap(), binary.BigEndian.Uint16(p[:])), nil
}

func socksReply(w io.Writer, code byte) error {
	// Reply with an all-zero IPv4 bound address; clients do not need it for
	// CONNECT.
	_, err := w.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}
