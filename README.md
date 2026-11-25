# Tailconnect

[![CI](https://github.com/vynxc/tailconnect/actions/workflows/ci.yml/badge.svg)](https://github.com/vynxc/tailconnect/actions/workflows/ci.yml)

Tailconnect pairs an Android phone with an agent over a **serverless Tailcat tunnel**. It is deliberately **not a VPN**:

- no `VpnService`
- no TUN device
- no route or DNS changes
- no Android VPN slot or key icon
- only traffic explicitly sent through Tailconnect uses the tunnel

The phone is the Tailcat server. The agent is the Tailcat client. One code gives both directions:

- **Agent → phone LAN:** the agent can reach TVs, routers, NAS boxes, wireless-debugging ports, and other devices on the phone's current Wi-Fi/Ethernet network.
- **Phone → agent services:** the agent can publish local or LAN services back to the phone as `http://localhost:<port>` bookmarks.


## Install

### 1. Install the Android app

Download the latest signed APK:

<https://github.com/vynxc/tailconnect/releases/latest>

```bash
adb install -r Tailconnect-0.3.0.apk
```

Open **Tailconnect**, grant notifications, and tap **Start**. The app shows a
13-character code. **Keep this code across restarts** is on by default; use
**Rotate** if you ever need a new one.

### 2. Install the CLI on the agent

With Go:

```bash
go install github.com/vynxc/tailconnect/cmd/tailconnect@latest
export PATH="$PATH:$(go env GOPATH)/bin"
tailconnect version
```

Or download the matching `tailconnect-*` binary from the release page and put it
on `PATH`.

### 3. Install the agent skill

No npm package is needed. `skills.sh` reads this GitHub repo directly; there is
no package for you to publish.

```bash
npx skills add vynxc/tailconnect --skill tailconnect -g -a opencode -a claude-code -y
```

Preview without installing:

```bash
npx skills add vynxc/tailconnect --list
```

Omit `-g` for a project-local install. `npx` needs Node.js 22.20 or newer.

If you do not want to use Node, copy the skill manually:

```bash
mkdir -p ~/.config/opencode/skills/tailconnect
curl -fsSL https://raw.githubusercontent.com/vynxc/tailconnect/main/skills/tailconnect/SKILL.md \
  -o ~/.config/opencode/skills/tailconnect/SKILL.md
```

For Claude Code, use `~/.claude/skills/tailconnect/SKILL.md` instead.

### 4. Pair

```bash
tailconnect save ABCD-EFGH-JKMNP
tailconnect agent --name workstation --socks 127.0.0.1:1080
```

The agent prints the phone network, discovered devices, wireless-debugging
commands, and any services the phone can open.

That is the whole install. No account, no control server, no VPN.

## Architecture

```text
Phone (Android)                              Agent machine
┌──────────────────────────────────┐         ┌──────────────────────────────────┐
│ Tailconnect app                  │         │ tailconnect CLI + agent skill    │
│                                  │         │                                  │
│ tailcat.Server  ◄────────────────┼─ WG/DERP┼─► tailcat.Client                │
│   │                              │         │      │                           │
│   ├─ LAN forwarder ──────────────┼─────────┼──────┘  agent → phone LAN         │
│   │                              │         │                                  │
│   └─ smux server  ◄──────────────┼─ stream─┼─ smux client                     │
│         │                        │         │      │                           │
│   localhost:PORT  ◄──────────────┼─────────┼──────┘  phone → agent service     │
└──────────────────────────────────┘         └──────────────────────────────────┘
```

## How the pairing code works

A code looks like this:

```text
ABCD-EFGH-JKMNP
```

- 13 characters from Hideout's unambiguous 30-character alphabet:
  `23456789ABCDEFGHJKMNPQRSTVWXYZ`
- 12 secret characters plus one DERP-region residue character.
- The 12-character secret carries about 58.9 bits of entropy.
- Both sides derive three keys with Argon2id:
  - 3 passes
  - 64 MiB memory
  - 1 thread
  - 96 bytes: server key, WireGuard PSK, client key
- The server allowlists the client key derived from the code, so leaking the
  server's `tc…` address alone is not enough to connect.
- The last character is `DERP region ID mod 30`. The agent fetches the official
  Tailcat DERP map and tries every region matching that residue.
- No database, account, coordination API, or short-link service is involved.
- A code is ephemeral: stop the service and the keys are gone.

The only infrastructure is **official Tailcat**: `tailcat.dev` for the DERP map
and Tailscale's public DERP relays. Direct peer-to-peer UDP is used when hole
punching succeeds; DERP is the fallback. No other server is contacted.

## CLI

Build:

```bash
make build
```

Run the phone-side server on a Linux machine or Android/Termux for testing:

```bash
./bin/tailconnect serve --allow 192.168.1.0/24
```

It prints a code such as `ABCD-EFGH-JKMNP`. On the agent:

```bash
./bin/tailconnect agent --code ABCD-EFGH-JKMNP \
  --name workstation \
  --socks 127.0.0.1:1080 \
  --expose web=127.0.0.1:3000
```

### Reach the phone's LAN

Use the local SOCKS5 proxy:

```bash
curl --socks5-hostname 127.0.0.1:1080 http://192.168.1.60:8060/
```

Or create an explicit TCP forward:

```bash
./bin/tailconnect agent --code ABCD-EFGH-JKMNP \
  --forward 5555=192.168.1.50:5555
```

### Automatic phone context and discovery

When the agent connects, the phone automatically sends a `PhoneContext` over the
same tunnel. The agent prints it without extra commands:

- phone model and app version
- Wi-Fi/Ethernet interface, phone IPv4 address, LAN prefix, gateway, DNS
- reverse service ports and `http://localhost:<port>` URLs
- wireless-debugging endpoints discovered over mDNS
- other mDNS services on the phone's LAN (Google Cast, AirPlay, HTTP, printers,
  SMB, SSH, and similar)

The agent automatically creates local `127.0.0.1` forwards for discovered
wireless-debugging endpoints and prints the exact commands. Disable that with
`--no-auto-adb`.

### Wireless ADB

If the phone advertises Wireless debugging over mDNS, the agent prints the
ready-to-run commands. Otherwise, forward the two ports manually. Wireless
debugging uses a pairing port and a later debug port:

```bash
# Pair (use the pairing port shown under Wireless debugging)
./bin/tailconnect agent --code ABCD-EFGH-JKMNP \
  --forward 37123=192.168.1.50:37123
adb pair 127.0.0.1:37123

# Connect (use the debug port shown after pairing)
./bin/tailconnect agent --code ABCD-EFGH-JKMNP \
  --forward 5555=192.168.1.50:5555
adb connect 127.0.0.1:5555
```


The same works for debugging the phone itself: use the phone's own Wi-Fi IP as
the forward target. For someone else's phone, they enable Wireless debugging and
give you the pairing code and ports.

### Persistent code

By default the Android app now keeps the same code across service restarts. The
secret is stored encrypted with Android Keystore; it is not written to logs or
backups. The app has:

- **Keep this code across restarts** — enabled by default
- **Rotate** — clears the saved key and starts a new session

The agent can also save the code locally:

```bash
tailconnect save ABCD-EFGH-JKMN3
tailconnect agent --name workstation --socks 127.0.0.1:1080
tailconnect forget
```

The phone-side CLI can reuse a code too:

```bash
tailconnect serve --code ABCD-EFGH-JKMN3 --allow 192.168.1.0/24
```

The saved code lives at:

```text
$XDG_CONFIG_HOME/tailconnect/pair.json
```

or the platform user-config directory, with mode `0600`.

A permanent code is a long-lived credential. Rotate it if it is shared or
leaked. The code still requires the official Tailcat DERP map to resolve its
region residue.

### Publish an agent service to the phone

```bash
./bin/tailconnect agent --code ABCD-EFGH-JKMNP \
  --expose web=127.0.0.1:3000 \
  --expose nas=192.168.1.20:5000
```

The phone app creates local listeners and shows:

```text
web  http://localhost:3000/
nas  http://localhost:5000/
```

The phone cannot ask the agent to dial an arbitrary target: only service IDs the
agent advertised with `--expose` are accepted.

## Android app

The Android app is a native `Activity` + foreground service. The Go core is
packaged as an executable in `jniLibs` and run from the app's native library
directory. There is **no VpnService** and no VPN permission.

Build the arm64 binary and debug APK:

```bash
make android-bin
cd android
./gradlew :app:assembleDebug
```

APK:

```text
android/app/build/outputs/apk/debug/app-debug.apk
```

Install:

```bash
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

The app:

- starts the server as a `specialUse` foreground service
- uses a dark Material 3 UI (`Theme.Material3.Dark.NoActionBar`) with M3 cards,
  filled/outlined buttons, and color roles
- detects the active Wi-Fi/Ethernet IPv4 subnet and allowlists it
- sends phone model, network, gateway, DNS, reverse service ports, and mDNS
  discovery results to the agent automatically
- shows the code, shareable link, connected agent, and agent service bookmarks
- marks the window `FLAG_SECURE` and keeps the code out of notifications
- stores an optional persistent code with Android Keystore AES-GCM
- lets you optionally allow the phone's localhost (for ADB and local servers)
- stops the process and invalidates an ephemeral code when you press Stop;
  a persistent code survives until you press Rotate

`arm64-v8a` is the default and needs no NDK. For an x86_64 emulator build, set
`ANDROID_NDK_HOME` and run `make android-bin-x86` before the APK build; Go requires
cgo/external linking for Android/amd64.

## Security model

- The code is a session password. Treat it like one.
- The server only admits the client key derived from the code.
- The code, tailcat address, and keys are redacted from logs.
- LAN access is an explicit allowlist of private IPv4 prefixes. Loopback is
  separate opt-in. Link-local, multicast, CGNAT, public internet, and Tailcat's
  own ULA are never forwarded.
- Agent services are an explicit allowlist on the agent. The phone cannot make
  the agent dial anything else.
- No analytics, crash reporting, automatic updater, or third-party service.
- Official Tailcat DERP relays see connection metadata, not WireGuard payloads.
  Direct P2P is preferred.

## Limitations

- 13-character codes are session credentials by default. A persistent code is a
  long-lived credential; rotate it if it leaks. The persisted Android key is
  protected by Android Keystore, but the code is still a bearer secret.
- Agent services are TCP. The phone-side LAN forwarder supports TCP and unicast
  UDP; the CLI's SOCKS proxy and `--forward` are TCP-only for now.
- mDNS discovery is best-effort and uses Android's `NsdManager`. SSDP/UPnP is not
  implemented yet. Use known IPs and ports when discovery misses a device.
- IPv4 LAN prefixes only.
- HTTPS services exposed through `localhost` may produce certificate warnings.
  HTTP and WebSocket work best.
- The Android app is arm64/x86_64 and needs real-device validation.
- Persistent codes are tied to the code's DERP-region residue. If that residue
  disappears from the official map, rotate the code.
- OpenTailcat was used as an architectural reference for Android/Tailcat
  integration, but no VPN code is included.

## Troubleshooting

**No code in the app**  
Tap **Start** and wait for the server to pick a DERP region. If it errors, check
network connectivity and try **Rotate**.

**Agent cannot connect**  
Make sure the phone service is running, the code is current, and the agent can
reach official Tailcat DERP. If the phone used **Rotate**, update the saved agent
code with `tailconnect save`.

**No LAN devices**  
The phone must be on Wi-Fi or Ethernet. Cellular has no LAN to share. Check the
app's **Shared network** line; it should show the phone's IPv4 prefix.

**ADB is not auto-forwarded**  
Enable **Wireless debugging** on the target phone. mDNS can miss on some devices;
use the manual `--forward` commands in the skill.

**Skill not found by the agent**  
Preview with `npx skills add vynxc/tailconnect --list`, then install globally
with `-g` or copy `skills/tailconnect/SKILL.md` into your agent's skills folder.

## Development

```bash
make test
go vet ./...
./scripts/e2e-local.sh   # uses official Tailcat DERP; needs network
```

Requirements:

- Go 1.27.4+
- JDK 17+
- Android SDK 36 for the APK
- Gradle wrapper 9.4.1 (downloaded automatically)

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Credits

- [Tailcat](https://github.com/tailscale/tailcat) by Tailscale, BSD-3-Clause.
- [Hideout](https://github.com/hideouthq/hideout) for the short-code/region-residue
  design. No GPL code is copied; the implementation here is independent.
- [OpenTailcat](https://github.com/OmarAlaaeldein/OpenTailcat), Apache-2.0, for
  the Android/Tailcat integration patterns and proof that this is possible.
- [xtaci/smux](https://github.com/xtaci/smux), MIT, for stream multiplexing.
