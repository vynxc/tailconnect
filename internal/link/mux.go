package link

import (
	"time"

	"github.com/xtaci/smux"
)

// muxConfig keeps dead tunnels from lingering for the default 30s+ keepalive
// window. Both sides use the same values; smux negotiates its own version.
func muxConfig() *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.KeepAliveInterval = 5 * time.Second
	cfg.KeepAliveTimeout = 15 * time.Second
	return cfg
}
