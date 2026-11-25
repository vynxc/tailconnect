// Command tailconnect pairs an Android phone and an agent over a serverless
// Tailcat tunnel. The phone exposes selected LAN destinations; the agent can
// also publish local services back to the phone.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vynxc/tailconnect/internal/link"
	"github.com/vynxc/tailconnect/internal/paircode"
	"github.com/vynxc/tailconnect/internal/protocol"
)

const version = "0.2.0"

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "agent":
		agent(os.Args[2:])
	case "parse":
		parse(os.Args[2:])
	case "version", "--version", "-version":
		fmt.Printf("tailconnect %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `tailconnect %s

  tailconnect serve [flags]
      Run the phone-side tunnel server on this machine and print a code.
      The Android app runs the same server; this command is for testing and
      for using a Linux/Android device as the network gateway.

  tailconnect agent --code CODE [flags]
      Connect to the phone, expose local services to it, and optionally
      provide a local SOCKS5 proxy and port forwards into the phone's LAN.

  tailconnect parse CODE
      Normalize a code and print its link and DERP residue.

Examples:
  tailconnect serve --allow 192.168.1.0/24
  tailconnect serve --allow 192.168.1.0/24 --allow-loopback
  tailconnect agent --code ABCD-EFGH-JKMNP --socks 127.0.0.1:1080
  tailconnect agent --code ABCD-EFGH-JKMNP \
      --forward 5555=192.168.1.50:5555 \
      --expose web=127.0.0.1:3000

`, version)
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	secret := fs.String("secret", "", "12-character secret; empty generates one")
	allow := &stringList{}
	fs.Var(allow, "allow", "private IPv4 LAN prefix or address to share (repeatable, comma-separated)")
	allowLoopback := fs.Bool("allow-loopback", false, "also allow the phone-side localhost (dangerous)")
	controlPort := fs.Uint("control-port", uint(protocolControlPort()), "tailcat control port")
	parentPID := fs.Int("parent-pid", 0, "exit if this parent PID goes away (Android service)")
	phoneName := fs.String("phone-name", "", "phone name shown to the agent")
	appVersion := fs.String("app-version", version, "phone app version shown to the agent")
	networkType := fs.String("network-type", "unknown", "wifi, ethernet, cellular, or unknown")
	lanAddress := fs.String("lan-address", "", "phone's own LAN IPv4 address")
	lanPrefix := fs.String("lan-prefix", "", "phone's LAN IPv4 prefix")
	gateway := fs.String("gateway", "", "phone's default gateway")
	dns := &stringList{}
	fs.Var(dns, "dns", "phone DNS server (repeatable)")
	jsonState := fs.Bool("json", false, "print machine-readable JSON lines")
	verbose := fs.Bool("verbose", false, "enable Tailcat networking logs")
	_ = fs.Parse(args)

	policy, err := link.ParsePolicy(allow.values, *allowLoopback)
	if err != nil {
		fatal(err)
	}
	if *phoneName == "" {
		*phoneName, _ = os.Hostname()
	}
	if *lanPrefix == "" && len(policy.Prefixes) > 0 {
		*lanPrefix = policy.Prefixes[0].String()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *parentPID > 0 {
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if os.Getppid() != *parentPID {
						stop()
						return
					}
				}
			}
		}()
	}

	var jsonMu sync.Mutex
	srv, err := link.StartServer(ctx, link.ServerConfig{
		Secret:      *secret,
		Policy:      policy,
		ControlPort: uint16(*controlPort),
		Verbose:     *verbose,
		PhoneName:   *phoneName,
		AppVersion:  *appVersion,
		NetworkType: *networkType,
		LANAddress:  *lanAddress,
		LANPrefix:   *lanPrefix,
		Gateway:     *gateway,
		DNSServers:  dns.values,
		OnState: func(state link.ServerState) {
			if *jsonState {
				jsonMu.Lock()
				defer jsonMu.Unlock()
				enc := json.NewEncoder(os.Stdout)
				_ = enc.Encode(map[string]any{"type": "state", "state": state})
				return
			}
			fmt.Printf("\nagent: %s\n", agentLine(state.Agent))
			for _, svc := range state.Services {
				fmt.Printf("  service %-12s %-24s %s\n", svc.ID, svc.Label, svc.URL)
			}
		},
	})
	if err != nil {
		fatal(err)
	}
	defer srv.Close()
	go func() { _ = srv.ReadContextInput(os.Stdin) }()

	started := map[string]any{
		"type":        "started",
		"code":        srv.Code(),
		"link":        paircode.Link(srv.Code()),
		"policy":      srv.Policy().String(),
		"controlPort": srv.ControlPort(),
		"region":      srv.RegionID(),
	}
	if *jsonState {
		jsonMu.Lock()
		_ = json.NewEncoder(os.Stdout).Encode(started)
		jsonMu.Unlock()
	} else {
		fmt.Printf("tailconnect %s\n", version)
		fmt.Printf("code:    %s\n", srv.Code())
		fmt.Printf("link:    %s\n", paircode.Link(srv.Code()))
		fmt.Printf("policy:  %s\n", srv.Policy())
		fmt.Printf("control: %d\n", srv.ControlPort())
		fmt.Printf("region:  %d\n", srv.RegionID())
		fmt.Printf("\nWaiting for the agent. Ctrl-C stops and invalidates the code.\n")
	}
	<-ctx.Done()
}

func agent(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	code := fs.String("code", "", "pairing code or tailconnect://pair/ link")
	name := fs.String("name", "", "agent name shown on the phone")
	socks := fs.String("socks", "", "local SOCKS5 listen address, e.g. 127.0.0.1:1080")
	noAutoADB := fs.Bool("no-auto-adb", false, "do not create local forwards for discovered wireless-debugging endpoints")
	expose := &stringList{}
	forward := &stringList{}
	fs.Var(expose, "expose", "id=host:port service to publish to the phone (repeatable)")
	fs.Var(forward, "forward", "localport=phonehost:port forward (repeatable)")
	controlPort := fs.Uint("control-port", uint(protocolControlPort()), "tailcat control port")
	jsonState := fs.Bool("json", false, "print state changes as JSON lines")
	verbose := fs.Bool("verbose", false, "enable Tailcat networking logs")
	_ = fs.Parse(args)
	if *code == "" {
		*code = os.Getenv("TAILCONNECT_CODE")
	}
	if *code == "" {
		fatal(fmt.Errorf("--code or TAILCONNECT_CODE is required"))
	}

	parsed, err := paircode.ParseLink(*code)
	if err != nil {
		fatal(err)
	}
	exposes, err := parseExposes(expose.values)
	if err != nil {
		fatal(err)
	}
	forwards, err := parseForwards(forward.values)
	if err != nil {
		fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var jsonMu sync.Mutex
	c, err := link.StartClient(ctx, link.ClientConfig{
		Code:           parsed.Code,
		Name:           *name,
		Expose:         exposes,
		Forwards:       forwards,
		SOCKSListen:    *socks,
		ControlPort:    uint16(*controlPort),
		Verbose:        *verbose,
		DisableAutoADB: *noAutoADB,
		OnContext: func(ctx protocol.PhoneContext, adb []link.ADBForward) {
			if *jsonState {
				jsonMu.Lock()
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
					"type": "context", "context": ctx, "adb": adb,
				})
				jsonMu.Unlock()
				return
			}
			printContext(ctx, adb)
		},
		OnState: func(state link.ClientState) {
			if *jsonState {
				jsonMu.Lock()
				_ = json.NewEncoder(os.Stdout).Encode(state)
				jsonMu.Unlock()
				return
			}
			fmt.Printf("connected=%v region=%d path=%s agent=%s\n", state.Connected, state.RegionID, state.Path, state.Agent)
		},
	})
	if err != nil {
		fatal(err)
	}
	defer c.Close()

	fmt.Printf("tailconnect agent %s\n", version)
	fmt.Printf("code: %s\n", paircode.Format(parsed.Code))
	if *socks != "" {
		fmt.Printf("socks: %s\n", *socks)
	}
	for i, f := range forwards {
		fmt.Printf("forward[%d]: %s -> %s\n", i, f.Listen, f.Target)
	}
	for _, e := range exposes {
		fmt.Printf("expose: %s -> %s\n", e.ID, e.Target)
	}
	fmt.Printf("\nCtrl-C disconnects.\n")
	<-ctx.Done()
}

func printContext(ctx protocol.PhoneContext, adb []link.ADBForward) {
	if ctx.Phone != "" {
		fmt.Printf("\nphone: %s", ctx.Phone)
		if ctx.App != "" {
			fmt.Printf(" (app %s)", ctx.App)
		}
		fmt.Println()
	}
	if ctx.Network.Type != "" || ctx.Network.Prefix != "" || ctx.Network.Address != "" {
		fmt.Printf("network: %s", ctx.Network.Type)
		if ctx.Network.Interface != "" {
			fmt.Printf(" %s", ctx.Network.Interface)
		}
		if ctx.Network.Address != "" {
			fmt.Printf(" %s", ctx.Network.Address)
		}
		if ctx.Network.Prefix != "" {
			fmt.Printf(" (%s)", ctx.Network.Prefix)
		}
		if ctx.Network.Gateway != "" {
			fmt.Printf(" gateway %s", ctx.Network.Gateway)
		}
		fmt.Println()
	}
	if len(ctx.Services) > 0 {
		fmt.Println("phone services:")
		for _, svc := range ctx.Services {
			fmt.Printf("  %-12s %-24s %s\n", svc.ID, svc.Label, svc.URL)
		}
	}
	if len(ctx.ADB) > 0 || len(adb) > 0 {
		fmt.Println("wireless debugging:")
		for _, f := range adb {
			if f.Kind == "pair" {
				fmt.Printf("  adb pair %s   -> %s (pairing code required)\n", f.Local, f.Target)
			} else {
				fmt.Printf("  adb connect %s -> %s\n", f.Local, f.Target)
			}
		}
	}
	if len(ctx.Discovered) > 0 {
		fmt.Println("discovered on the phone LAN:")
		for _, svc := range ctx.Discovered {
			fmt.Printf("  %-28s %-22s %s:%d\n", svc.Name, svc.Type, svc.Host, svc.Port)
		}
	}
}

func parse(args []string) {
	if len(args) != 1 {
		fatal(fmt.Errorf("usage: tailconnect parse CODE"))
	}
	p, err := paircode.ParseLink(args[0])
	if err != nil {
		fatal(err)
	}
	fmt.Printf("code:    %s\n", paircode.Format(p.Code))
	fmt.Printf("link:    %s\n", paircode.Link(p.Code))
	fmt.Printf("secret:  %s\n", p.Secret)
	fmt.Printf("residue: %d\n", p.Residue)
}

type stringList struct{ values []string }

func (s *stringList) String() string { return strings.Join(s.values, ",") }
func (s *stringList) Set(v string) error {
	s.values = append(s.values, v)
	return nil
}

func parseExposes(values []string) ([]link.Expose, error) {
	var out []link.Expose
	for _, v := range values {
		id, target, ok := strings.Cut(v, "=")
		if !ok || strings.TrimSpace(id) == "" || strings.TrimSpace(target) == "" {
			return nil, fmt.Errorf("--expose wants id=host:port, got %q", v)
		}
		out = append(out, link.Expose{ID: strings.TrimSpace(id), Target: strings.TrimSpace(target)})
	}
	return out, nil
}

func parseForwards(values []string) ([]link.Forward, error) {
	var out []link.Forward
	for _, v := range values {
		listen, target, ok := strings.Cut(v, "=")
		if !ok || listen == "" || target == "" {
			return nil, fmt.Errorf("--forward wants localport=phonehost:port, got %q", v)
		}
		if !strings.Contains(listen, ":") {
			listen = "127.0.0.1:" + listen
		}
		out = append(out, link.Forward{Listen: listen, Target: target})
	}
	return out, nil
}

func agentLine(a link.AgentState) string {
	if !a.Connected {
		return "disconnected"
	}
	path := a.Path
	if path == "" {
		path = "connecting"
	}
	return fmt.Sprintf("%s (%s)", a.Name, path)
}

func protocolControlPort() uint16 { return 7000 }

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "tailconnect:", err)
	os.Exit(1)
}
