# Native AdBlock

Open **Routing → AdBlock** (or **sing-box → Routing → AdBlock** for sing-box), enable filtering and save. The default source is
StevenBlack's hosts list. You can use your own public HTTP/HTTPS lists, add
custom entries, and configure automatic refreshes (1–168 hours).

Filtering runs inside the selected Xray or sing-box core. It requires no extra
DNS daemon, root access on clients, system hosts-file changes, or HTTPS
interception. It applies to traffic that actually passes through that core;
standalone sidecars that do not relay their traffic through the core are not
covered.

## Rules

| Entry                                              | Meaning                    |
| -------------------------------------------------- | -------------------------- |
| `0.0.0.0 ads.example.com alias.example.com`        | Block both exact hostnames |
| `127.0.0.1 ads.example.com` / `:: ads.example.com` | Block the exact hostname   |
| `ads.example.com` / `full:ads.example.com`         | Block that exact hostname  |
| `domain:example.com` / `                           |                            | example.com^` | Block the hostname and its subdomains             |
| `@@                                                |                            | example.com^` | Unconditional domain exception within that source |

Comments, duplicate entries, and unsupported browser rules are ignored.
Ordinary-address hosts mappings (for example `192.0.2.1 example.com`) are
redirects and are ignored. IP addresses are never interpreted as hostnames.
Use Punycode for international domain names.

The panel allowlist excludes a hostname and its subdomains from **AdBlock**.
It does not force direct access or override other administrator routing rules.
For consistent Xray/sing-box behavior, a suffix rule covering an explicitly
allowed child is omitted entirely. Independent exact-host rules remain active.
For example, allowing `safe.example.com` removes `domain:example.com`, while a
separate `ads.example.com` entry remains blocked.

## Runtime and updates

Xray uses a high-priority domain rule and a blackhole outbound. Runtime inbound
sniffing recovers HTTP Host, TLS SNI and QUIC names. When enabling sniffing on
an inbound that previously had it disabled, `routeOnly` preserves the original
connection destination. Existing sniffing options are retained where possible.

sing-box uses native `sniff` and `reject` actions with the named inline rule set
`3x-ui-adblock`. The panel applies these after the native routing template and
removes them from the editor snapshot, so template overrides and editor saves
cannot accidentally bypass or permanently embed filtering. This uses native
rule actions available since sing-box 1.11, and inline rule sets since 1.10.
The project targets sing-box 1.14.

Settings and downloaded lists are committed in a single database transaction.
Failed sources retain their own working caches. Missing caches, oversized lists
and cancelled builds leave the previous complete list and settings intact. Limits are 32 sources, 32 MiB per
source and 300,000 entries. A refresh has a five-minute deadline. Unchanged
lists do not restart the core. A failed core restart is retried by the scheduler
after five minutes, including when filtering has been disabled.

Filtering cannot reliably identify IP-only connections without an exposed
hostname, TLS ECH-hidden names, or advertising delivered through the same
hostname/HTTPS connection as the main content. Cosmetic and URL-path filtering
remain browser/client functions.

## Verification

```sh
go test ./internal/adblock ./internal/web/service ./internal/web/controller
cd frontend && npm run typecheck && npm run build:ci
```

Optional real-core HTTP tests use IP-addressed SOCKS connections and check exact
matches, suffix matches, allowlisted hosts and ordinary traffic:

```sh
XUI_ADBLOCK_XRAY_BINARY=/path/to/xray \
XUI_ADBLOCK_SINGBOX_BINARY=/path/to/sing-box \
go test ./internal/web/service -run TestAdBlockRealCores -v
```

Core configuration checks run before each traffic test. A runtime denied access
to netlink sockets is explicitly reported as skipped after configuration
validation, rather than counted as a successful traffic test.

## Automatic profiles and YouTube

The profile selector supplies maintained sources and a refresh interval:

| Profile             | Source                | Default refresh |
| ------------------- | --------------------- | --------------- |
| Balanced            | StevenBlack hosts     | 24 hours        |
| Mobile ads          | AdGuard DNS filter    | 12 hours        |
| Extended protection | OISD big              | 12 hours        |
| Light               | OISD small            | 24 hours        |
| Custom              | User-provided sources | User-configured |

Selecting a preset fills in its sources and enables automatic refreshes. The
backend resolves preset IDs to canonical URLs. Editing the source field changes
the profile to Custom. Custom blocked/allowed entries remain editable with any
profile. Changes take effect after saving.

Downloaded source entries are cached separately from custom entries. Changes to
custom entries, exceptions or the YouTube mode reuse that cache when the source
configuration is unchanged. They do not depend on source availability and do
not falsely update the last successful source-refresh timestamp. Clicking
Update lists, and scheduled refreshes, always download current lists.

Three YouTube policies are available:

- **General rules:** apply the configured lists without automatic YouTube
  exceptions.
- **Compatibility:** exclude shared video, API and image domains from AdBlock,
  avoiding domain rules that break playback.
- **Compatibility + separate Google advertising domains:** keep the playback
  exceptions and add exact-host rules for ancillary Google advertising hosts.
  This filters those requests, not YouTube video ad segments.

These are domain policies, not a YouTube video-ad remover. In a browser,
video-ad removal needs content filtering on the device. In the official app,
Xray/sing-box cannot distinguish an encrypted advertising segment from regular
video on the same hostname. Blocking `googlevideo.com` is therefore not an
acceptable video-ad filtering implementation. See the primary explanation:
https://adguard.com/en/article/how-to-block-ads-on-youtube.html

Failed sources keep their working copies and expose the error, attempt count and
retry deadline in the panel. Automatic retries use delays of 5, 10, 20, 40, 80,
160, 320 and at most 360 minutes. The scheduler checks every 15 seconds;
disabling auto-updates stops scheduled downloads. Manual refreshes can run
immediately. Successful downloads clear the failure state. Local cache-based
saves leave it intact until a source refresh succeeds.

## Apply state, pause and connection scope

Saving an interval or profile description does not restart the core. A restart
is scheduled only when the effective domains, enabled state or scope change.
The apply intent and error are stored in the database. The UI distinguishes
saved settings from settings awaiting application and offers **Retry apply**.
Failed application is retried after five minutes; restarting the panel does not
lose it. Changing the selected core also schedules application to that core.

**Pause for 5/15/60 minutes** keeps the enabled preference and persists an UTC
deadline. Expiry resumes filtering independently of the list-update preference,
normally within one 15-second scheduler tick after expiry. An ongoing refresh
or core restart can delay application until that operation finishes (refresh
deadline: five minutes). Startup performs its first scheduler check after five seconds.
If the core cannot restart, the UI keeps showing an application error.

Inbound and client scope each support **all**, **only selected** and
**all except selected**. Both conditions must match. An empty **only selected**
list filters no connections. The global scope gates every profile. Ordered
connection policies can assign Balanced, Mobile, Extended, Light or **Off** to
specific clients/inbounds; the first enabled matching policy wins. Unmatched
connections use the common profile. Up to eight policies are supported. Each
profile keeps an independent domain list: a lighter profile never inherits the
common profile’s stricter domains. Shared custom domains, exceptions and YouTube
compatibility apply to all filtering profiles; Off disables AdBlock for matching
connections without bypassing administrator routing. A disabled policy is
skipped entirely. Moving a policy changes its priority. Xray identifies
clients by email, sing-box by authenticated user name. Client matching requires
the protocol to expose that identity; for anonymous/tunnel traffic select the
inbound instead. Xray client exclusions enumerate the remaining users in the
generated config, so adding a client takes effect on the next core regeneration.
An authenticated Xray inbound with missing email identities rejects client
exclusions instead of bypassing other routing rules. Exclusions do not route
traffic directly and do not bypass administrator routing.

**Domain check** examines saved settings, exact/suffix matching, allowlist,
YouTube compatibility, scope and pause. It shows matched rules and source names.
It does not probe the network and does not claim to diagnose other routing rules.
When application is pending, its result explicitly warns that live behavior may
differ. Hostnames can be entered in Unicode and are normalized to Punycode.

## Independent source caches

Each source keeps a validated domain cache, ETag, Last-Modified, content date,
check date and error. Refreshes send conditional headers and reuse cached content
on HTTP 304. A failed source uses its own last working copy while successful
sources update. Partial refreshes retain warnings and retry with backoff even if
the refresh interval has not elapsed. The UI displays the age and error per source.
A source without a working copy must load successfully before a new configuration
can be committed. Cancellation and domain/size-limit violations preserve the
previous complete configuration. Removing a source also removes its cache on the
next build. Legacy combined caches remain usable for local settings changes;
per-source metadata is populated by the next successful refresh.

Per-source caches have a combined budget of 1,200,000 entries / 64 MiB to
limit memory use when many overlapping sources are configured.

## YouTube browser companion

The AdBlock routing tab offers an authenticated download of the bundled
**3x-ui YouTube Companion** extension for Chromium 111+, Chrome and Edge.
Extract the ZIP, open the browser extension manager, enable developer mode and
load the extracted directory containing manifest.json. Reload existing YouTube
tabs. The popup enables/disables filtering and stores that choice locally.

The Manifest V3 extension runs only on HTTPS www.youtube.com/m.youtube.com.
It removes known advertising fields from same-origin player/next JSON responses
and initial player data while preserving media URLs, captions and playability
data. A fallback clicks visible skip controls and seeks finite, actively playing
video only when YouTube explicitly marks the player as showing an ad. Cosmetic
rules hide known promoted renderers. It requests only local storage permission,
collects no credentials and contacts no external service. Server profile changes
do not automatically change the extension’s local setting.

This is experimental browser filtering. YouTube delivery changes, server-side
ad insertion and anti-adblock behavior may prevent removal; fixture tests are
not a guarantee for every live video. The official Android/iOS YouTube apps are
not covered. No certificate installation or interception of encrypted app traffic
is required.

## Server-side video filtering

An experimental native server mode is available as `x-ui youtube-proxy`.
The routing tab provides downloadable Xray/sing-box fragments. Unlike the
browser companion, the proxy performs player response filtering on the server,
requires participating clients to trust its CA. Managed mode starts from the
AdBlock tab and follows the ordinary switch, scope, ordered profiles and pause. See [setup, researched approaches and
limits](youtube-server-filter.md). It is not guaranteed for official mobile
apps or advertisements already stitched into the video stream.
