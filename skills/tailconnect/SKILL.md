---
name: tailconnect
description: Pair an Android phone to this agent through a serverless Tailcat tunnel. Use when the user gives a Tailconnect code (for example ABCD-EFGH-JKMNP) and wants to reach devices on the phone's LAN, debug a phone or TV over ADB/TCP, or publish a local service to the phone. This is not a VPN and must never start a VpnService or change routes.
---

# Tailconnect

Tailconnect is an app-level tunnel. The Android phone runs a Tailcat server and
the agent runs a Tailcat client. Only traffic explicitly sent through the
`tailconnect` command uses the tunnel. Do not create a VPN, TUN, route, DNS
change, or `VpnService`.

## Prerequisites

The `tailconnect` binary must be available. In this repository:

```bash
make build
export PATH="$PWD/bin:$PATH"
```

## Pair

The user reads a 13-character code from the phone app and gives it to you, for
example `ABCD-EFGH-JKMNP`. Run the agent in the background so it stays connected:

```bash
tailconnect agent \
  --code ABCD-EFGH-JKMNP \
  --name "$(hostname)" \
  --socks 127.0.0.1:1080
```

Prefer passing the code through the environment if the command line may be
observed:

```bash
TAILCONNECT_CODE=ABCD-EFGH-JKMNP tailconnect agent --name "$(hostname)" --socks 127.0.0.1:1080
```

Keep this process alive for the duration of the task. Never log, echo, commit,
or paste the code into a public place.

If the user wants this to survive restarts, save the code once:

```bash
tailconnect save ABCD-EFGH-JKMN3
```

Then future runs can omit `--code`:

```bash
tailconnect agent --name "$(hostname)" --socks 127.0.0.1:1080
```

`tailconnect forget` removes the saved code. The phone app's persistent-code
option must also be enabled for the same code to survive phone service restarts.

## Automatic context

As soon as the agent connects, the phone sends its model, network, LAN prefix,
gateway, reverse service ports, wireless-debugging endpoints, and mDNS-discovered
LAN services. The agent prints them automatically. Do not ask the user for those
values if they are already in the output.

## Reach the phone's LAN

The local SOCKS5 proxy accepts IP addresses on the phone's network:

```bash
curl --socks5-hostname 127.0.0.1:1080 http://192.168.1.60:8060/
```

For tools that do not support SOCKS, create an explicit TCP forward:

```bash
tailconnect agent --code "$TAILCONNECT_CODE" \
  --forward 5555=192.168.1.50:5555
```

Then use `127.0.0.1:5555` locally.

## Wireless ADB

If the agent output already shows:

```text
adb connect 127.0.0.1:<port> -> <phone>:<port>
adb pair 127.0.0.1:<port> -> <phone>:<port> (pairing code required)
```

use those commands directly. The agent creates those local forwards from the
phone's mDNS discovery. Otherwise, use the manual forwarding below. Android
Wireless debugging uses a pairing port and a separate debug port.

```bash
# 1. Pair
tailconnect agent --code "$TAILCONNECT_CODE" \
  --forward 37123=192.168.1.50:37123
adb pair 127.0.0.1:37123

# 2. Connect
tailconnect agent --code "$TAILCONNECT_CODE" \
  --forward 5555=192.168.1.50:5555
adb connect 127.0.0.1:5555
adb devices
```

Replace `192.168.1.50` with the phone's LAN IP. For debugging the phone that is
running Tailconnect itself, use that phone's own Wi-Fi IP. For someone else's
phone, ask them for the pairing code and ports.

## TV and other LAN devices

Use the SOCKS proxy with a known IP and port:

```bash
curl --socks5-hostname 127.0.0.1:1080 http://192.168.1.60:8060/
```

Prefer the services already listed under "discovered on the phone LAN". Do not
scan a network the user does not own or have permission to test.

## Publish a service to the phone

The phone app shows bookmarks for services the agent advertises:

```bash
tailconnect agent --code "$TAILCONNECT_CODE" \
  --name workstation \
  --expose web=127.0.0.1:3000 \
  --expose nas=192.168.1.20:5000
```

The phone opens `http://localhost:<assigned-port>/`. The phone can only request
service IDs you advertised; it cannot make the agent dial arbitrary targets.

## Stop

Stop the background `tailconnect agent` process. Stopping the phone-side service
invalidates an ephemeral code. If the phone uses a persistent code, press
**Rotate** in the app to invalidate it as well.

## Safety

- The pairing code is a session password.
- Only selected TCP/UDP destinations are forwarded.
- No general internet exit: the agent cannot use the phone as an internet VPN,
  and the phone cannot use the agent as one.
- If the user asks for a VPN, remind them Tailconnect intentionally is not one.
