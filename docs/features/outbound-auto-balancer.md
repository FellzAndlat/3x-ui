# Automatic selection for outbound subscriptions

Open **Xray settings → Outbounds → Outbound subscriptions**, add or edit a
subscription. New subscriptions automatically enable **Automatic outbound
selection** and **Prepend**, so no manual routing-tag setup is required. The same setting
works with the selected Xray or sing-box core.

The subscription exposes a stable outbound tag, `sub-auto-ID` (ID is its
numeric subscription ID). For custom routes, select this tag as an outbound
in routing rules. **Prepend** makes the automatic route the default for unmatched
traffic. Existing routing rules still take precedence. The `sub-auto-` tag
namespace is reserved for these generated outbounds.

## Modes

- **Lowest latency:** an authenticated HTTP request through the actual proxy
  measures connection establishment and response time. Only HTTP 2xx responses
  count as healthy. The default URL is `https://www.google.com/generate_204`.
  A healthy current node is retained if it is within the configured tolerance
  (default 50 ms) of the fastest node.
- **Highest download speed:** the built-in URL is
  `https://speed.cloudflare.com/__down?bytes=1048576`. Optionally configure an
  HTTP(S) file URL serving at least 64 KiB. Each node downloads at most 1 MiB per check, sequentially, with a
  10-second request deadline. Speed is measured in Mbit/s during body transfer.
  A healthy current node is retained unless another is more than 10% faster.
  Tiny/error responses are rejected rather than mistaken for high throughput.

The default check interval is 300 seconds. Latency mode allows 30–86400 seconds;
speed mode allows 300–86400 seconds. Scheduling has a 10-second granularity.

Three independent timers are used:

- **Ranking:** default 300 seconds; compare candidates by latency or speed.
- **Planned switching:** default 900 seconds (15 minutes), configurable from
  30 to 86400 seconds. Select the best healthy alternative even if the current
  node remains fastest. Retain the current node if no alternative is healthy,
  and retry at the next switching period. The timer restarts after a switch.
  Speed mode revalidates health and reuses the last speed measurements unless
  the speed-ranking timer is also due, keeping download frequency bounded.
- **Availability:** default 30 seconds, configurable from 10 to 300 seconds.
  Check the current node using a small HTTP request through the proxy.
  A timeout, connection error or non-2xx response triggers failover checks
  without waiting for either of the other timers. Speed mode uses the default
  small HTTP probe for health and emergency checks, not the download file.
  Long speed scans recheck the current node between batches of two candidates;
  a failed health check interrupts the speed scan and starts failover.

A failed-node replacement is selected by availability and latency first;
emergency checks stop at the first healthy group of up to eight alternatives
to avoid waiting for a large subscription. Subsequent ranking checks can
improve it by speed. These checks are HTTP proxy
probes, not ICMP echo requests. Detection includes the probe timeout, candidate
checks and API application time; it is not instantaneous.
**Check now** forces a check of that subscription. Updated subscription content
schedules a fresh check. Disabled subscriptions are not checked.

The subscription list shows the selected node, last check time, next planned switch and switch reason. Hover over
the `sub-auto-ID` tag for per-node delays, speeds and failures. Results and the
selection survive panel restarts. These measurements describe the panel
server's path through each outbound, not a phone's direct connection to it.

## Runtime behavior

Xray uses a permanent loopback outbound with an internal inbound tag. Its
first routing rule points at the selected member; switching updates routing
through the running core's API. The panel checks the core's actual routing
decision after applying the change. sing-box uses a native selector, changes
it through the local Clash API and reads back the confirmed selection.
Selection changes do not restart the core or replace outbound handlers.
Existing connections stay on their original node; new connections use the
new selection. Connections through a node that goes offline cannot be migrated.

Adding/removing subscription members or enabling this feature still requires
the panel's normal configuration reload to install the handlers. If a live
update fails, the list shows the error separately from the saved selection;
the scheduler retries every 10 seconds. A stopped core is never started by
balancing; startup generates the saved selection. Custom sing-box controller
ports and secrets are preserved. The default controller is local-only at
`127.0.0.1:10090`; automatic selection refuses remote controllers.

Before the first check, the first compatible member is used. After a health
check finds no healthy members (or a selected member disappears), a dedicated
block outbound receives traffic; it never falls back to a direct connection.
A failed speed download triggers a fresh small health check and latency-based
selection, with a warning, rather than treating every node as offline. A
subsequent successful check restores speed ranking.

Checks use isolated temporary core processes and loopback SOCKS listeners.
The sing-box probe process cannot adopt or stop the production core. Only
candidates and their dialer dependencies are included in probe configurations.
Checks do not overlap; a busy manual outbound test defers an automatic check.
A concurrent subscription edit invalidates an in-flight selection result.

Unsupported sing-box subscription members are skipped in its generated runtime
configuration; stored members are kept for switching back to Xray. Existing
manual outbounds continue to use the core's normal validation.

## API

The existing outbound subscription create/update form accepts `autoBalance`,
`balanceMode` (`latency` or `speed`), `probeURL`, `probeInterval` (seconds), and
`tolerance` (milliseconds), `switchInterval` and `healthInterval` (seconds).
Omitting `autoBalance` on updates preserves existing settings
for older API clients. List responses include `selectedTag`, `lastProbe`,
`probeError`, `probeResults` (a JSON-encoded result array), `lastHealth`,
`lastSwitch`, `switchReason`, `appliedTag`, and `applyError`. The applied tag
is the core-confirmed member (or the dedicated block tag). New columns give existing subscriptions
900-second switching and 30-second availability intervals on migration.

`POST /panel/api/xray/outbound-subs/:id/probe` checks an enabled automatic
subscription immediately and returns its current state.

## Integration verification

To run real-core switching tests on a Linux host:

```sh
XRAY_E2E_BINARY=/path/to/xray go test ./internal/xray -run TestAutomaticLoopbackLiveSwitch_E2E -v
SINGBOX_E2E_BINARY=/path/to/sing-box go test ./internal/singbox -run TestSelectorLiveSwitch_E2E -v
```

Both tests check new connections, blocking and recovery, and retention of an
established TCP connection. sing-box needs access to the host's network
interface monitor (netlink); a sandbox that denies it cannot run its live test.
