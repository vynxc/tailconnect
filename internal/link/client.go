package link

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tailscale/tailcat"
	"github.com/xtaci/smux"
	"go4.org/mem"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"

	"github.com/vynxc/tailconnect/internal/paircode"
	"github.com/vynxc/tailconnect/internal/protocol"
)

const (
	connectAttemptTimeout = 12 * time.Second
	reconnectMin          = 1 * time.Second
	reconnectMax          = 15 * time.Second
)

// Expose is an agent-side service the phone may open.
type Expose struct {
	ID            string
	Label         string
	Target        string // host:port on the agent or its LAN
	PreferredPort int
}

// Forward is a local TCP listener on the agent that dials a phone-side target.
type Forward struct {
	Listen string // e.g. 127.0.0.1:5555
	Target string // e.g. 192.168.1.50:5555
}

type ClientConfig struct {
	Code string
	Name string

	Expose      []Expose
	Forwards    []Forward
	SOCKSListen string

	ControlPort uint16
	Logf        func(string, ...any)
	OnState     func(ClientState)
	Verbose     bool
}

type ClientState struct {
	Connected bool           `json:"connected"`
	RegionID  int            `json:"regionId,omitempty"`
	Path      string         `json:"path,omitempty"`
	Agent     string         `json:"agent,omitempty"`
	Services  []ServiceState `json:"services,omitempty"`
}

type Client struct {
	cfg      ClientConfig
	parsed   paircode.Parsed
	material paircode.Material
	logf     func(string, ...any)

	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	tc        *tailcat.Client
	sess      *smux.Session
	state     ClientState
	closed    chan struct{}
	closeOnce sync.Once

	socks    *SocksServer
	forwards []*tcpForward
}

func StartClient(parent context.Context, cfg ClientConfig) (*Client, error) {
	if cfg.ControlPort == 0 {
		cfg.ControlPort = protocol.DefaultControlPort
	}
	if cfg.Name == "" {
		cfg.Name, _ = os.Hostname()
	}
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}
	parsed, err := paircode.ParseLink(cfg.Code)
	if err != nil {
		return nil, err
	}
	material, err := paircode.Derive(parsed.Secret)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	c := &Client{
		cfg:      cfg,
		parsed:   parsed,
		material: material,
		logf:     func(format string, args ...any) { logf(paircode.Redact(fmt.Sprintf(format, args...))) },
		ctx:      ctx,
		cancel:   cancel,
		closed:   make(chan struct{}),
	}
	if err := c.startLocalListeners(); err != nil {
		cancel()
		c.closeLocalListeners()
		return nil, err
	}
	go c.run()
	return c, nil
}

func (c *Client) State() ClientState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
		c.closeLocalListeners()
		c.mu.Lock()
		if c.sess != nil {
			c.sess.Close()
		}
		if c.tc != nil {
			c.tc.Close()
		}
		c.mu.Unlock()
		close(c.closed)
	})
	return nil
}

func (c *Client) run() {
	backoff := reconnectMin
	for {
		if err := c.connect(); err != nil {
			if c.ctx.Err() != nil {
				return
			}
			c.logf("connect: %v", err)
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < reconnectMax {
				backoff *= 2
				if backoff > reconnectMax {
					backoff = reconnectMax
				}
			}
			continue
		}
		backoff = reconnectMin
		select {
		case <-c.ctx.Done():
			return
		case <-c.closed:
			return
		case <-c.sessionDone():
			c.logf("tunnel closed; reconnecting")
			c.mu.Lock()
			c.state.Connected = false
			c.state.Path = ""
			c.mu.Unlock()
			c.emit()
		}
	}
}

func (c *Client) sessionDone() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return c.sess.CloseChan()
}

func (c *Client) connect() error {
	c.mu.Lock()
	oldTC, oldSess := c.tc, c.sess
	c.tc, c.sess = nil, nil
	c.state.Connected = false
	c.state.Path = ""
	c.mu.Unlock()
	c.emit()
	if oldSess != nil {
		oldSess.Close()
	}
	if oldTC != nil {
		oldTC.Close()
	}

	addrs, err := c.addresses(c.ctx)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return errors.New("no DERP region matches the code")
	}
	c.logf("trying %d relay candidate(s)", len(addrs))

	clientKey := key.NodePrivateFromRaw32(mem.B(c.material.ClientPrivate[:]))

	var lastErr error
	for _, cand := range addrs {
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		attemptCtx, cancel := context.WithTimeout(c.ctx, connectAttemptTimeout)
		tcLogf := func(string, ...any) {}
		if c.cfg.Verbose {
			tcLogf = c.logf
		}
		tc := &tailcat.Client{Server: cand.addr, Key: clientKey, Logf: tcLogf}
		conn, err := tc.DialTCPPort(attemptCtx, c.cfg.ControlPort)
		cancel()
		if err != nil {
			lastErr = err
			tc.Close()
			continue
		}
		sess, err := smux.Client(conn, muxConfig())
		if err != nil {
			conn.Close()
			tc.Close()
			lastErr = err
			continue
		}
		if err := c.sendManifest(sess); err != nil {
			sess.Close()
			conn.Close()
			tc.Close()
			lastErr = err
			continue
		}
		c.mu.Lock()
		c.tc = tc
		c.sess = sess
		c.state = ClientState{Connected: true, RegionID: int(cand.regionID), Agent: c.cfg.Name}
		c.mu.Unlock()
		c.emit()
		c.logf("connected via region %d", cand.regionID)
		go c.acceptAgentStreams(sess)
		go c.watchPath(tc)
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("no relay candidate answered")
	}
	return lastErr
}

func (c *Client) sendManifest(sess *smux.Session) error {
	stream, err := sess.OpenStream()
	if err != nil {
		return err
	}
	defer stream.Close()
	m := protocol.Manifest{
		Version:  protocol.Version,
		Agent:    c.cfg.Name,
		Services: make([]protocol.Service, 0, len(c.cfg.Expose)),
	}
	seen := map[string]bool{}
	for _, e := range c.cfg.Expose {
		id := strings.TrimSpace(e.ID)
		if id == "" || !safeID(id) || seen[id] {
			return fmt.Errorf("bad or duplicate expose id %q", e.ID)
		}
		seen[id] = true
		target := strings.TrimSpace(e.Target)
		if _, _, err := net.SplitHostPort(target); err != nil {
			return fmt.Errorf("expose %s target %q: %w", id, target, err)
		}
		port := e.PreferredPort
		if port == 0 {
			if _, p, err := net.SplitHostPort(target); err == nil {
				fmt.Sscanf(p, "%d", &port)
			}
		}
		if port < 1024 || port > 65535 {
			port = 0
		}
		label := e.Label
		if label == "" {
			label = id
		}
		m.Services = append(m.Services, protocol.Service{
			ID: id, Label: label, Protocol: "tcp", PreferredPort: port,
		})
	}
	return protocol.WriteMessage(stream, m)
}

func (c *Client) acceptAgentStreams(sess *smux.Session) {
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			return
		}
		go c.serveAgentStream(stream)
	}
}

func (c *Client) serveAgentStream(stream *smux.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(10 * time.Second))
	h, err := protocol.ReadHeader(stream)
	if err != nil {
		return
	}
	_ = stream.SetDeadline(time.Time{})
	var target string
	for _, e := range c.cfg.Expose {
		if e.ID == h.Service {
			target = e.Target
			break
		}
	}
	if target == "" {
		return
	}
	remote, err := net.DialTimeout("tcp", target, dialTimeout)
	if err != nil {
		return
	}
	tailcat.ProxyConns(stream, remote)
}

func (c *Client) watchPath(tc *tailcat.Client) {
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
		ctx, cancel := context.WithTimeout(c.ctx, 3*time.Second)
		pong, err := tc.DiscoPing(ctx)
		cancel()
		if err != nil {
			continue
		}
		path := "relay"
		if pong.Endpoint != "" {
			path = "direct"
		}
		c.mu.Lock()
		if c.tc == tc {
			c.state.Path = path
		}
		c.mu.Unlock()
		c.emit()
	}
}

func (c *Client) emit() {
	if c.cfg.OnState != nil {
		c.cfg.OnState(c.State())
	}
}

func (c *Client) dialTCP(ctx context.Context, ap netip.AddrPort) (net.Conn, error) {
	c.mu.Lock()
	tc := c.tc
	c.mu.Unlock()
	if tc == nil {
		return nil, errors.New("not connected")
	}
	return tc.DialTCP(ctx, ap)
}

type candidate struct {
	addr     tailcat.Addr
	regionID tailcfg.DERPRegionID
}

func (c *Client) addresses(ctx context.Context) ([]candidate, error) {
	dm, err := tailcat.FetchDERPMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch DERP map: %w", err)
	}
	serverKey := key.NodePrivateFromRaw32(mem.B(c.material.ServerPrivate[:]))
	var addrs []candidate
	for id, region := range dm.Regions {
		if int(id)%len(paircode.Alphabet) != c.parsed.Residue {
			continue
		}
		ci := tailcat.ConnInfo{
			ServerPublic:      tailcat.NodePublic{NodePublic: serverKey.Public()},
			ServerDiscoPublic: tailcat.DiscoPublicForNode(serverKey),
			PresharedKey:      tailcat.PresharedKey(c.material.PresharedKey),
			Region:            []*tailcfg.DERPRegion{region},
		}
		addrs = append(addrs, candidate{addr: ci.Addr(), regionID: id})
	}
	sort.Slice(addrs, func(i, j int) bool { return addrs[i].regionID < addrs[j].regionID })
	return addrs, nil
}
