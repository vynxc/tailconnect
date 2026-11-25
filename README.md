# Tailconnect

Tailconnect pairs an Android phone with an agent over a **serverless Tailcat tunnel**. It is deliberately **not a VPN**:

- no `VpnService`
- no TUN device
- no route or DNS changes
- no Android VPN slot or key icon
- only traffic explicitly sent through Tailconnect uses the tunnel

The phone is the Tailcat server. The agent is the Tailcat client. One code gives both directions:

- **Agent → phone LAN:** the agent can reach TVs, routers, NAS boxes, wireless-debugging ports, and other devices on the phone's current Wi-Fi/Ethernet network.
- **Phone → agent services:** the agent can publish local or LAN services back to the phone as `http://localhost:<port>` bookmarks.

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

### Wireless ADB

ADB does not speak SOCKS, so use a forward. Wireless debugging uses two ports:
the pairing port shown on the phone, and the later debug port.

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
- detects the active Wi-Fi/Ethernet IPv4 subnet and allowlists it
- shows the code, shareable link, connected agent, and agent service bookmarks
- marks the window `FLAG_SECURE` and keeps the code out of notifications
- lets you optionally allow the phone's localhost (for ADB and local servers)
- stops the process and invalidates the code when you press Stop

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

- 13-character codes are for sessions. Stop/rotate them; do not treat them as
  permanent credentials.
- Agent services are TCP. The phone-side LAN forwarder supports TCP and unicast
  UDP; the CLI's SOCKS proxy and `--forward` are TCP-only for now.
- mDNS/SSDP discovery is not implemented. Use known IPs and ports.
- IPv4 LAN prefixes only.
- HTTPS services exposed through `localhost` may produce certificate warnings.
  HTTP and WebSocket work best.
- The Android app is arm64 only and needs real-device validation.
- OpenTailcat was used as an architectural reference for Android/Tailcat
  integration, but no VPN code is included.

## Development

```bash
make test
go vet ./...
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
