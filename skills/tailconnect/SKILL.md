---
name: tailconnect
description: Pair an Android phone to this agent through a serverless Tailcat tunnel. Use when the user gives a Tailconnect code (for example ABCD-EFGH-JKMNP) and wants to reach devices on the phone's LAN, debug a phone or TV over ADB/TCP, publish a local service to the phone, or keep a persistent pairing across restarts. This is not a VPN and must never start a VpnService or change routes.
---

# Tailconnect

Tailconnect gives an agent access to the phone's current LAN, and gives the
phone access to services on the agent, through one serverless Tailcat tunnel.

It is **not a VPN**:

- never start a `VpnService`
- never create a TUN device
- never change routes or DNS
- only traffic explicitly sent through `tailconnect` uses the tunnel

The phone is the Tailcat server. The agent is the Tailcat client. One
13-character code replaces an account and a control plane.

## What the skill can do

Use it when the user wants to:

- reach a TV, router, NAS, printer, camera, or another phone on the phone's LAN
- use wireless ADB against the phone or another Android device
- publish a local dev server to the phone as `http://localhost:<port>`
- reconnect to the same phone after a restart
- debug a Roku or other LAN device over HTTP/ECP/TCP

Do **not** use it to expose the phone as a general internet exit node. It is
not a VPN and the phone-side policy refuses public-internet destinations.

## Prerequisites

The user needs:

1. **The Android app v0.3.0 or newer** installed from
   <https://github.com/vynxc/tailconnect/releases/latest>
2. The app started, with the code visible
3. A `tailconnect` CLI v0.3.0 or newer on the agent
4. This skill installed

If the user has no APK installed, give them the release link above and tell them
to install `Tailconnect-*.apk`, open it, grant notifications, and tap **Start**.

If the phone is on an older APK, the automatic `PhoneContext` and persistent
code features may be missing. Ask the user to update before troubleshooting those.

### Install the CLI

Preferred if Go is available:

```bash
go install github.com/vynxc/tailconnect/cmd/tailconnect@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

Or download the matching binary from the release page and put it on `PATH`.

Check:

```bash
tailconnect version
```

If `tailconnect` is not on `PATH`, use the absolute path to the downloaded
binary for the rest of this skill.

## First pairing

Ask the user for the 13-character code shown in the app. Do not invent or guess
a code.

```bash
tailconnect agent \
  --code ABCD-EFGH-JKMNP \
  --name "$(hostname)" \
  --socks 127.0.0.1:1080
```

Keep this process running for the task. If the shell tool supports background
processes, start it in the background and keep the handle. Prefer
`tailconnect save` or the `TAILCONNECT_CODE` environment variable over putting
the code in shell history.

The agent prints:

- phone model and app version
- phone network type, interface, address, prefix, gateway, DNS
- phone-published reverse services and their `http://localhost:<port>` URLs
- wireless-debugging endpoints
- other mDNS-discovered LAN services
- automatically created local ADB forwards, when discovered

Do not ask the user for values that are already in this output.

## Persistent pairing

Save the code once:

```bash
tailconnect save ABCD-EFGH-JKMNP
```

Then reconnect without `--code`:

```bash
tailconnect agent --name "$(hostname)" --socks 127.0.0.1:1080
```

The saved code is stored at:

```text
$XDG_CONFIG_HOME/tailconnect/pair.json
```

with mode `0600`. Remove it with:

```bash
tailconnect forget
```

On the phone, **Keep this code across restarts** must be on for the code to
survive phone-side restarts. If the user presses **Rotate**, update the saved
agent code.

## Read the phone context

The phone sends a `PhoneContext` as soon as the agent connects. Use it:

| Field | How to use it |
| --- | --- |
| `phone` | Tell the user which device is paired |
| `network.address` / `prefix` | Know the phone's LAN. Use `prefix` to pick targets |
| `network.gateway` | Router/admin page target |
| `network.dns` | Phone-side DNS servers, if needed |
| `services` | Reverse services; the phone user taps these |
| `adb` | Wireless-debugging endpoints; use the printed local commands |
| `discovered` | mDNS services; use the host and port through SOCKS |

Prefer the discovered host/port over scanning. Do not scan networks without the
user's permission.

## Recipe: reach a service on the phone LAN

Start/keep the agent running with SOCKS:

```bash
tailconnect agent \
  --code ABCD-EFGH-JKMNP \
  --name "$(hostname)" \
  --socks 127.0.0.1:1080
```

Then use the local SOCKS proxy:

```bash
curl --socks5-hostname 127.0.0.1:1080 http://192.168.1.60:8060/
```

For a tool that does not speak SOCKS, create an explicit forward:

```bash
tailconnect agent \
  --code ABCD-EFGH-JKMNP \
  --forward 8060=192.168.1.60:8060
```

Then use `127.0.0.1:8060` locally.

## Recipe: wireless ADB against the phone or another device

If the context output contains:

```text
adb connect 127.0.0.1:42619 -> 192.168.1.50:5555
adb pair 127.0.0.1:44463   -> 192.168.1.50:37123 (pairing code required)
```

use those commands directly. The agent created those forwards from the phone's
mDNS discovery.

If discovery did not find the device, forward the two ports manually. Wireless
debugging uses a pairing port and a later debug port:

```bash
# Pair
tailconnect agent \
  --code ABCD-EFGH-JKMNP \
  --forward 37123=192.168.1.50:37123
adb pair 127.0.0.1:37123

# Connect
tailconnect agent \
  --code ABCD-EFGH-JKMNP \
  --forward 5555=192.168.1.50:5555
adb connect 127.0.0.1:5555
adb devices
```

For another phone, ask the user to enable **Wireless debugging**, then give you
the pairing code and the two ports shown there.

## Recipe: publish a local service to the phone

```bash
tailconnect agent \
  --code ABCD-EFGH-JKMNP \
  --name workstation \
  --expose web=127.0.0.1:3000 \
  --expose nas=192.168.1.20:5000
```

The phone app shows:

```text
web  http://localhost:3000/
nas  http://localhost:5000/
```

Tell the user to tap the bookmark. The phone can only request service IDs the
agent advertised; it cannot make the agent dial arbitrary targets.

HTTP and WebSocket work best. HTTPS services opened through `localhost` may show
a certificate warning.

## Recipe: TV, Roku, or other LAN device

Use SOCKS or a forward to the device's known IP and port. Example Roku:

```bash
# Device information
curl --socks5-hostname 127.0.0.1:1080 http://192.168.1.50:8060/query/device-info

# Active app
curl --socks5-hostname 127.0.0.1:1080 http://192.168.1.50:8060/query/active-app
```

Roku notes:

- `/query/apps` may be blocked by the Roku's **Limited mode** setting.
- The developer web UI may require a separate device password.
- The debug console on port 8085 is output-only; do not treat it as a shell.
- Prefer the services listed under `discovered` before probing ports.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| `invalid pairing code` | The code must be 13 characters, no ambiguous `0/1/I/L/O/U` |
| `no DERP region matches residue` | The persistent code's region residue is gone; rotate the phone code |
| Connected but no LAN | Phone is on a cellular connection, or LAN sharing is off; check the app's shared network |
| SOCKS connection failed | The target is outside the phone's allowlisted prefix |
| ADB auto-forward missing | Enable Wireless debugging; mDNS can miss on some devices; use manual forwards |
| `adb pair` works but `adb connect` fails | Use the debug port shown after pairing, not the pairing port |
| Phone user cannot open a service | Ensure the agent is still connected and the service is listed in the app |
| Code changed | The phone pressed **Rotate** or persistence is off; run `tailconnect save` again |

## Safety

- The code is a session password. Never log, echo, commit, or publish it.
- A persistent code is a long-lived bearer credential. Use **Rotate** if it leaks.
- Use `tailconnect forget` to remove the agent-side saved code.
- Do not scan networks the user does not own or have permission to test.
- Do not enable or create a VPN. If the user asks for one, explain this project
  intentionally is not a VPN.

## Output style

When you report back to the user:

- say whether the tunnel is connected and whether it is direct or relayed
- summarize the phone network and discovered devices, not every raw field
- give the exact ready-to-run ADB commands when available
- keep pairing codes out of logs, summaries, commits, and screenshots
- ask only for information the phone did not already send

## Install this skill elsewhere

No npm package is required. The skills.sh CLI reads this repository directly:

```bash
npx skills add vynxc/tailconnect --skill tailconnect -g -a opencode -a claude-code -y
```

Project-local install:

```bash
npx skills add vynxc/tailconnect --skill tailconnect -a opencode
```

Preview without installing:

```bash
npx skills add vynxc/tailconnect --list
```
