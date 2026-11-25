package link

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tailscale/tailcat"
	"github.com/xtaci/smux"
	"go4.org/mem"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/wgengine/filter"

	"github.com/vynxc/tailconnect/internal/paircode"
	"github.com/vynxc/tailconnect/internal/protocol"
)

const (
	defaultControlPort = protocol.DefaultControlPort
	manifestTimeout    = 10 * time.Second
	maxServices        = 32
	maxAgentName       = 64
	maxServiceID       = 64
	maxServiceLabel    = 96
	dialTimeout        = 10 * time.Second
	udpIdleTimeout     = 2 * time.Minute
)

// ServiceState is a phone-facing view of an agent service.
type ServiceState struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Port  int    `json:"port"`
	URL   string `json:"url"`
}

// AgentState is the currently connected agent.
type AgentState struct {
	Connected bool   `json:"connected"`
	Name      string `json:"name,omitempty"`
	Path      string `json:"path,omitempty"` // "direct", "relay", or ""
}

// ServerState is a snapshot for the Android UI and CLI.
type ServerState struct {
	Code     string         `json:"code"`
	Policy   string         `json:"policy"`
	Agent    AgentState     `json:"agent"`
	Services []ServiceState `json:"services"`
}

type ServerConfig struct {
	// Secret is the 12-character part of the code. If empty, one is generated.
	Secret string

	// Policy is the private IPv4 allowlist for agent -> phone LAN traffic.
	Policy Policy

	// ControlPort is the tailcat port used by the agent for the reverse stream
	// mux. Zero uses 7000.
	ControlPort uint16

	// Logf receives redacted diagnostics. If nil, log.Printf is used.
	Logf func(string, ...any)

	// OnState is called whenever the connected agent or its services change.
	OnState func(ServerState)

	// Verbose enables Tailcat's internal networking logs.
	Verbose bool

	// Context is shown to the agent when it connects. Android fills these from
	// Build.MODEL, LinkProperties and the foreground service.
	PhoneName   string
	AppVersion  string
	NetworkType string
	LANAddress  string
	LANPrefix   string
	Gateway     string
	DNSServers  []string
}

type Server struct {
	srv    *tailcat.Server
	ln     net.Listener
	code   string
	region tailcfg.DERPRegionID
	addr   tailcat.Addr
	policy Policy
	port   uint16
	logf   func(string, ...any)

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	reverse *reverseSession
	agent   AgentState
	onState func(ServerState)

	ctxMu         sync.Mutex
	contextSess   *smux.Session
	contextOut    *smux.Stream
	contextMu     sync.Mutex // serializes writes to contextOut
	discoveryADB  []protocol.ADBService
	discoveryMDNS []protocol.DiscoveredService

	phoneName  string
	appVersion string
	network    protocol.Network
}

func StartServer(parent context.Context, cfg ServerConfig) (*Server, error) {
	if cfg.ControlPort == 0 {
		cfg.ControlPort = defaultControlPort
	}
	if cfg.ControlPort < 1024 {
		return nil, fmt.Errorf("link: control port %d is privileged", cfg.ControlPort)
	}
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}
	secret := strings.ToUpper(strings.TrimSpace(cfg.Secret))
	if secret == "" {
		var err error
		secret, err = paircode.NewSecret()
		if err != nil {
			return nil, err
		}
	}
	material, err := paircode.Derive(secret)
	if err != nil {
		return nil, err
	}

	serverKey := key.NodePrivateFromRaw32(mem.B(material.ServerPrivate[:]))
	clientKey := key.NodePrivateFromRaw32(mem.B(material.ClientPrivate[:]))
	clientPub := clientKey.Public()

	ctx, cancel := context.WithCancel(parent)
	if cfg.NetworkType == "" {
		cfg.NetworkType = "unknown"
	}
	s := &Server{
		policy:     cfg.Policy,
		port:       cfg.ControlPort,
		logf:       func(format string, args ...any) { logf(paircode.Redact(fmt.Sprintf(format, args...))) },
		ctx:        ctx,
		cancel:     cancel,
		onState:    cfg.OnState,
		phoneName:  cfg.PhoneName,
		appVersion: cfg.AppVersion,
		network: protocol.Network{
			Type:    cfg.NetworkType,
			Address: cfg.LANAddress,
			Prefix:  cfg.LANPrefix,
			Gateway: cfg.Gateway,
			DNS:     append([]string(nil), cfg.DNSServers...),
		},
	}

	region, err := pickRegion(ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("link: pick DERP region: %w", err)
	}
	tcLogf := func(string, ...any) {}
	if cfg.Verbose {
		tcLogf = s.logf
	}
	tc := &tailcat.Server{
		Key:          serverKey,
		PresharedKey: tailcat.PresharedKey(material.PresharedKey),
		AllowClient:  func(k key.NodePublic) bool { return k == clientPub },
		Logf:         tcLogf,
		Region:       region,
		ServedTCPPorts: []filter.PortRange{
			{First: cfg.ControlPort, Last: cfg.ControlPort},
		},
		OnTCPForward:   s.onTCPForward,
		OnUDPForward:   s.onUDPForward,
		UDPIdleTimeout: udpIdleTimeout,
	}
	if err := tc.StartContext(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("link: start tailcat server: %w", err)
	}
	s.srv = tc
	s.addr = tc.TailcatAddr()
	s.region = region.RegionID
	s.code = paircode.Code(secret, int(region.RegionID))

	ln, err := tc.Listen(ctx, "tcp", fmt.Sprintf(":%d", cfg.ControlPort))
	if err != nil {
		tc.Close()
		cancel()
		return nil, fmt.Errorf("link: listen control port: %w", err)
	}
	s.ln = ln
	go s.accept()
	s.logf("listening; code %s; LAN policy %s", paircode.Format(s.code), cfg.Policy.String())
	return s, nil
}

func (s *Server) Code() string                   { return s.code }
func (s *Server) ControlPort() uint16            { return s.port }
func (s *Server) Policy() Policy                 { return s.policy }
func (s *Server) RegionID() tailcfg.DERPRegionID { return s.region }

// Addr returns the secret tailcat address. It must not be logged or shown in
// the UI; the pairing code is the user-facing form.
func (s *Server) Addr() tailcat.Addr { return s.addr }

func (s *Server) State() ServerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := ServerState{
		Code:   paircode.Format(s.code),
		Policy: s.policy.String(),
		Agent:  s.agent,
	}
	if s.reverse != nil {
		for _, svc := range s.reverse.services {
			state.Services = append(state.Services, svc)
		}
		sort.Slice(state.Services, func(i, j int) bool { return state.Services[i].ID < state.Services[j].ID })
	}
	return state
}

func (s *Server) Close() error {
	s.cancel()
	if s.ln != nil {
		s.ln.Close()
	}
	s.mu.Lock()
	if s.reverse != nil {
		s.reverse.close()
		s.reverse = nil
	}
	s.mu.Unlock()
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				s.logf("control accept: %v", err)
				return
			}
		}
		go s.handleControl(conn)
	}
}

func (s *Server) handleControl(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(manifestTimeout))
	sess, err := smux.Server(conn, muxConfig())
	if err != nil {
		s.logf("reverse mux: %v", err)
		return
	}
	stream, err := sess.AcceptStream()
	if err != nil {
		sess.Close()
		s.logf("reverse manifest stream: %v", err)
		return
	}
	var manifest protocol.Manifest
	if err := protocol.ReadMessage(stream, &manifest); err != nil {
		stream.Close()
		sess.Close()
		s.logf("reverse manifest: %v", err)
		return
	}
	stream.Close()
	_ = conn.SetDeadline(time.Time{})
	if err := validateManifest(manifest); err != nil {
		sess.Close()
		s.logf("reverse manifest rejected: %v", err)
		return
	}

	rs, err := newReverseSession(s, sess, manifest)
	if err != nil {
		sess.Close()
		s.logf("reverse services: %v", err)
		return
	}

	s.mu.Lock()
	old := s.reverse
	s.reverse = rs
	s.agent = AgentState{Connected: true, Name: manifest.Agent}
	s.mu.Unlock()
	if old != nil {
		old.close()
	}
	s.emit()

	// Open a phone -> agent context stream. It carries the phone's network,
	// reverse service ports and discovery results so the agent does not have
	// to ask for them.
	if ctxStream, err := sess.OpenStream(); err == nil {
		if herr := protocol.WriteHeader(ctxStream, protocol.StreamHeader{
			Version: protocol.Version, Kind: protocol.StreamKindContext,
		}); herr == nil {
			s.ctxMu.Lock()
			s.contextSess = sess
			s.contextOut = ctxStream
			s.ctxMu.Unlock()
			s.sendContext()
		} else {
			ctxStream.Close()
		}
	} else {
		s.logf("context stream: %v", err)
	}

	// Accept and discard agent-opened streams after the manifest. The phone is
	// the side that opens service streams; anything else is unexpected.
	go func() {
		for {
			st, err := sess.AcceptStream()
			if err != nil {
				return
			}
			st.Close()
		}
	}()

	select {
	case <-s.ctx.Done():
	case <-sess.CloseChan():
	}
	s.ctxMu.Lock()
	if s.contextSess == sess {
		if s.contextOut != nil {
			s.contextOut.Close()
		}
		s.contextSess = nil
		s.contextOut = nil
	}
	s.ctxMu.Unlock()
	s.mu.Lock()
	if s.reverse == rs {
		s.reverse = nil
		s.agent = AgentState{}
	}
	s.mu.Unlock()
	rs.close()
	s.emit()
}

func (s *Server) emit() {
	if s.onState == nil {
		return
	}
	s.onState(s.State())
}

func validateManifest(m protocol.Manifest) error {
	if m.Version != protocol.Version {
		return protocol.ErrBadVersion
	}
	if len(m.Agent) > maxAgentName {
		return errors.New("agent name too long")
	}
	if len(m.Services) > maxServices {
		return errors.New("too many services")
	}
	seen := map[string]bool{}
	for _, svc := range m.Services {
		if svc.ID == "" || len(svc.ID) > maxServiceID || !safeID(svc.ID) {
			return fmt.Errorf("bad service id %q", svc.ID)
		}
		if seen[svc.ID] {
			return fmt.Errorf("duplicate service id %q", svc.ID)
		}
		seen[svc.ID] = true
		if len(svc.Label) > maxServiceLabel {
			return fmt.Errorf("service %q label too long", svc.ID)
		}
		if svc.Protocol != "" && svc.Protocol != "tcp" {
			return fmt.Errorf("service %q protocol %q unsupported", svc.ID, svc.Protocol)
		}
		if svc.PreferredPort < 0 || svc.PreferredPort > 65535 {
			return fmt.Errorf("service %q port %d", svc.ID, svc.PreferredPort)
		}
	}
	return nil
}

func safeID(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) onTCPForward(dst netip.AddrPort) func(net.Conn) {
	if !s.policy.Allowed(dst) {
		return nil
	}
	return func(c net.Conn) {
		remote, err := net.DialTimeout("tcp4", dst.String(), dialTimeout)
		if err != nil {
			c.Close()
			return
		}
		tailcat.ProxyConns(c, remote)
	}
}

func (s *Server) onUDPForward(dst netip.AddrPort) func(tailcat.ConnPacketConn) {
	if !s.policy.Allowed(dst) {
		return nil
	}
	return func(c tailcat.ConnPacketConn) {
		remote, err := net.DialTimeout("udp4", dst.String(), dialTimeout)
		if err != nil {
			c.Close()
			return
		}
		pc, ok := remote.(tailcat.ConnPacketConn)
		if !ok {
			remote.Close()
			c.Close()
			return
		}
		tailcat.ProxyPacketConns(c, pc)
	}
}

// reverseSession owns the local listeners for one connected agent.
type reverseSession struct {
	server    *Server
	sess      *smux.Session
	manifest  protocol.Manifest
	services  []ServiceState
	byID      map[string]protocol.Service
	listens   []net.Listener
	closeOnce sync.Once
}

func newReverseSession(s *Server, sess *smux.Session, m protocol.Manifest) (*reverseSession, error) {
	rs := &reverseSession{
		server:   s,
		sess:     sess,
		manifest: m,
		byID:     make(map[string]protocol.Service, len(m.Services)),
	}
	for _, svc := range m.Services {
		rs.byID[svc.ID] = svc
		ln, err := listenLocal(svc.PreferredPort)
		if err != nil {
			rs.close()
			return nil, fmt.Errorf("service %s: %w", svc.ID, err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		rs.services = append(rs.services, ServiceState{
			ID:    svc.ID,
			Label: labelOrID(svc),
			Port:  port,
			URL:   fmt.Sprintf("http://localhost:%d/", port),
		})
		rs.listens = append(rs.listens, ln)
	}
	for i, ln := range rs.listens {
		svc := m.Services[i]
		go rs.acceptService(ln, svc.ID)
	}
	return rs, nil
}

func listenLocal(preferred int) (net.Listener, error) {
	if preferred >= 1024 && preferred <= 65535 {
		if ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", preferred)); err == nil {
			return ln, nil
		}
	}
	return net.Listen("tcp4", "127.0.0.1:0")
}

func (rs *reverseSession) acceptService(ln net.Listener, id string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go rs.open(conn, id)
	}
}

func (rs *reverseSession) open(conn net.Conn, id string) {
	stream, err := rs.sess.OpenStream()
	if err != nil {
		conn.Close()
		return
	}
	if err := protocol.WriteHeader(stream, protocol.StreamHeader{Version: protocol.Version, Service: id}); err != nil {
		stream.Close()
		conn.Close()
		return
	}
	tailcat.ProxyConns(stream, conn)
}

func (rs *reverseSession) close() {
	rs.closeOnce.Do(func() {
		for _, ln := range rs.listens {
			ln.Close()
		}
		rs.sess.Close()
	})
}

func labelOrID(svc protocol.Service) string {
	if strings.TrimSpace(svc.Label) == "" {
		return svc.ID
	}
	return svc.Label
}

// UpdateDiscovery replaces the discovery snapshot and pushes it to the agent.
func (s *Server) UpdateDiscovery(adb []protocol.ADBService, mdns []protocol.DiscoveredService) {
	s.ctxMu.Lock()
	s.discoveryADB = append([]protocol.ADBService(nil), adb...)
	s.discoveryMDNS = append([]protocol.DiscoveredService(nil), mdns...)
	s.ctxMu.Unlock()
	s.sendContext()
}

// ReadContextInput reads newline-delimited JSON from the Android process's
// stdin. Only discovery messages are currently defined; unknown types are
// ignored so the channel can grow without breaking old servers.
func (s *Server) ReadContextInput(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 512<<10)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var in protocol.ContextInput
		if err := json.Unmarshal([]byte(line), &in); err != nil {
			s.logf("context input: %v", err)
			continue
		}
		switch in.Type {
		case "discovery":
			s.UpdateDiscovery(in.ADB, in.MDNS)
		}
	}
	return sc.Err()
}

func (s *Server) sendContext() {
	ctx := s.contextSnapshot()
	s.ctxMu.Lock()
	out := s.contextOut
	s.ctxMu.Unlock()
	if out == nil {
		return
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	if err := protocol.WriteMessage(out, ctx); err != nil {
		s.ctxMu.Lock()
		if s.contextOut == out {
			s.contextOut = nil
			s.contextSess = nil
		}
		s.ctxMu.Unlock()
		out.Close()
	}
}

func (s *Server) contextSnapshot() protocol.PhoneContext {
	s.mu.Lock()
	services := make([]protocol.PublishedService, 0, len(s.reverseServices()))
	if s.reverse != nil {
		for _, svc := range s.reverse.services {
			services = append(services, protocol.PublishedService{
				ID: svc.ID, Label: svc.Label, Port: svc.Port, URL: svc.URL,
			})
		}
	}
	policy := s.policy
	s.mu.Unlock()
	sort.Slice(services, func(i, j int) bool { return services[i].ID < services[j].ID })

	s.ctxMu.Lock()
	adb := append([]protocol.ADBService(nil), s.discoveryADB...)
	mdns := append([]protocol.DiscoveredService(nil), s.discoveryMDNS...)
	s.ctxMu.Unlock()

	return protocol.PhoneContext{
		Version:       protocol.ContextVersion,
		Phone:         s.phoneName,
		App:           s.appVersion,
		Network:       s.network,
		Policy:        policy.String(),
		AllowLoopback: policy.AllowLoopback,
		ControlPort:   int(s.port),
		Region:        int(s.region),
		Services:      services,
		ADB:           adb,
		Discovered:    mdns,
	}
}

func (s *Server) reverseServices() []ServiceState {
	if s.reverse == nil {
		return nil
	}
	return s.reverse.services
}

func pickRegion(ctx context.Context) (*tailcfg.DERPRegion, error) {
	ci := tailcat.ConnInfo{RegionID: -1}
	if err := ci.Expand(ctx, tailcat.ExpandForServer); err != nil {
		return nil, err
	}
	if len(ci.Region) == 0 || ci.Region[0] == nil {
		return nil, errors.New("DERP map has no regions")
	}
	return ci.Region[0], nil
}

// MarshalState is a convenience for the Android bridge.
func (s *Server) MarshalState() ([]byte, error) {
	return json.Marshal(s.State())
}
