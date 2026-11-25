package link

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/tailscale/tailcat"
)

type tcpForward struct {
	ln     net.Listener
	target netip.AddrPort
	client *Client
}

func (c *Client) startLocalListeners() error {
	if c.cfg.SOCKSListen != "" {
		s, err := NewSocksServer(c.cfg.SOCKSListen, c.dialTCP, c.logf)
		if err != nil {
			return fmt.Errorf("link: socks listen: %w", err)
		}
		c.socks = s
		c.logf("SOCKS5 listening on %s", s.Addr())
	}
	for _, f := range c.cfg.Forwards {
		target, err := resolveAddrPort(f.Target)
		if err != nil {
			return fmt.Errorf("link: forward target %q: %w", f.Target, err)
		}
		listen := f.Listen
		if listen == "" {
			listen = "127.0.0.1:0"
		}
		ln, err := net.Listen("tcp", listen)
		if err != nil {
			return fmt.Errorf("link: forward listen %q: %w", f.Listen, err)
		}
		fw := &tcpForward{ln: ln, target: target, client: c}
		c.forwards = append(c.forwards, fw)
		c.logf("forwarding %s -> %s", ln.Addr(), target)
		go fw.accept()
	}
	return nil
}

func (c *Client) closeLocalListeners() {
	if c.socks != nil {
		c.socks.Close()
	}
	for _, f := range c.forwards {
		f.ln.Close()
	}
}

func (f *tcpForward) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *tcpForward) handle(conn net.Conn) {
	ctx, cancel := context.WithTimeout(f.client.ctx, dialTimeout)
	remote, err := f.client.dialTCP(ctx, f.target)
	cancel()
	if err != nil {
		conn.Close()
		return
	}
	tailcat.ProxyConns(conn, remote)
}

func resolveAddrPort(s string) (netip.AddrPort, error) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), nil
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return netip.AddrPort{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return netip.AddrPort{}, err
	}
	if len(ips) == 0 {
		return netip.AddrPort{}, fmt.Errorf("no IPv4 address for %q", host)
	}
	p, err := net.LookupPort("tcp", port)
	if err != nil {
		return netip.AddrPort{}, err
	}
	return netip.AddrPortFrom(ips[0], uint16(p)), nil
}
