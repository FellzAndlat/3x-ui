# Native YouTube server filter (experimental)

## Managed mode: Routing → AdBlock

The panel manages the proxy in-process; no extra binary, systemd unit or public
proxy port is required. Enable the ordinary AdBlock master switch, then enable
**Server YouTube filter**, choose participating inbounds/clients in the shared
scope and save. A fresh loopback listener is prepared before changing the core.
The generated config is checked by the selected core before application. On a
failed settings application the previous settings are restored and the core is
reconciled back to them; an unsuccessful rollback is reported explicitly.

Server filtering follows the common scope, ordered per-client policies, the
**Off** profile, the master switch and the 5/15/60-minute pause. The first matching
policy wins. Domain exceptions affect domain blocking; they do not override
server interception selection. Resume regenerates routes and starts the proxy
again. Ordinary core startup also prepares/commits the managed proxy. A listener
that unexpectedly stops schedules recovery through the existing retry mechanism.

The panel inserts YouTube-only TCP routes and corresponding UDP/QUIC rejection
rules to encourage TCP fallback. Infrastructure and existing managed domain
blocking precede interception. Ordinary user routes can be overridden for the
selected YouTube connections; inspect the final native config if custom routing
order is important. Domain recognition requires sniffing and can be defeated by
ECH/unknown hostnames or traffic that bypasses the core. Other hosts, including
`googlevideo.com`, remain encrypted and follow their usual routing.

Remove older manually installed `youtube-server-filter` / `youtube-quic-block`
outbounds/rules before enabling managed mode: conflicting tags are rejected.
The stored native template is preserved; generated managed state is added when
the core config is assembled. Disabling or pausing removes routes before the
working proxy is stopped. A failed core change keeps the prior proxy available.

## Certificate trust

Download **public CA** from the same tab and import it into the participating
browser/device's trusted certificate authorities. The panel shows its SHA-256
fingerprint and expiry. Verify the fingerprint through a trusted channel.
A VPN connection alone does not establish this trust. Firefox may use a separate
certificate store. Applications can reject user-installed roots or pin their
certificates; official YouTube Android/iOS app support is not promised.

The unique CA is stored in `<XUI_DB_FOLDER>/youtube-filter`. Its private file
`ca-private.pem` stays on the server with mode 0600; only `ca-cert.pem` can be
downloaded. Keep this directory during upgrades. Existing CA files are not
silently rotated; a replacement requires clients to trust a new certificate.
Expired/invalid or world-readable private CA files cause an explicit failure.

The server can access the participating YouTube HTTPS content while processing
it, including authentication cookies. It does not log request bodies, cookies,
credentials or media URLs. Retire client trust if the CA key is compromised.
Other HTTPS domains are not decrypted by this implementation.

## Upstream selection

Leave the upstream tag blank for direct server connections, or choose/type a
native outbound/endpoint tag. Autocomplete shows native template targets; tags
added by imported subscriptions may be entered manually. Xray balancer tags are
supported; sing-box selector/urltest targets are ordinary tagged outbounds.
The target must exist in the final generated config; missing targets cause an
error instead of falling back to direct egress.

A dedicated loopback SOCKS bridge on port 18081 routes the filter's upstream
HTTPS requests to the chosen target. Bridge traffic is excluded from filtering
so it cannot feed back into itself. A conflicting inbound on that port is
rejected. Video CDN traffic is still governed by ordinary core routing, rather
than the filter's upstream setting.

## Resource limits and diagnostics

Configure connection limit (8–512), rewrite workers (1–8), response cap
(1–16 MiB) and queue wait (0–2000 ms). Workers × response cap may not exceed
32 MiB before decoding; parsed JSON/decompression require additional memory.
Requests wait briefly for a rewrite slot and pass through unchanged if capacity
is exhausted. This favors playback availability over guaranteed ad removal.
The default is 128 connections, two workers, 8 MiB and 250 ms.

The panel polls running status, local listener address, upstream request count,
rewritten responses, busy skips, client TLS errors, upstream failures, malformed
responses, oversized responses and unsupported compression. The last error is a
sanitized category, without sensitive request data. A rewritten response proves
metadata removal, not that every video was ad-free. Counters belong to the current
process instance and reset when its limits/transport require a replacement.

Known gzip responses are supported and requests prefer identity encoding.
Malformed/unknown/oversized data is preserved byte-for-byte. Stale validators
are removed from rewritten data and its caching is disabled. Media bodies stream
without being decoded or buffered. WebSocket upgrades on intercepted hosts remain
unsupported; other hosts can carry them inside opaque TLS tunnels.

## Live playback QA

An opt-in Playwright scenario checks a normal video, Shorts and a live stream,
playback progress, normal-video seeking/captions and observed ad markers/metadata.
The default observation window is 30 seconds per scenario; `YOUTUBE_QA_SECONDS`
accepts 5–180 seconds. This short window cannot establish absence of all midroll ads.
It uses a fresh browser without logging into a YouTube account and saves a JSON
report. Supply three available video IDs; choose a normal video longer than
20 seconds that has captions, and an actual currently running live stream.

On the test machine, forward the **local address shown in the panel** through
SSH, copy the public CA, install frontend dependencies/Playwright Chromium, then:

```sh
cd frontend
YOUTUBE_QA_PROXY=http://127.0.0.1:18080 \
YOUTUBE_QA_CA=/path/to/ca-cert.pem \
YOUTUBE_QA_VIDEO=VIDEO_ID_11 \
YOUTUBE_QA_SHORT=SHORT_ID_11 \
YOUTUBE_QA_LIVE=LIVE_ID_11_ \
node tools/youtube-live-check.mjs
```

The example proxy port is the local forwarded port, not the automatically
allocated server port. Before browser launch the script verifies the filter's
leaf certificates with the supplied CA. Its temporary Chromium context accepts
only the SPKI hashes of those already verified certificates; global TLS
verification is not disabled. This QA setup does not install trust on your
normal browser. Real website access, media availability, captions and YouTube
consent/anti-bot flows can cause a test to fail independently of the filter.

The development environment used for this change could not reach live YouTube
(connection timeout). Live playback is therefore **not verified** here. Local
TLS fixtures and both core validators cover the implementation, while the live
scenario is provided for an environment with reachable YouTube.

## Approaches researched and limits

| Project | Approach | Applied here |
| --- | --- | --- |
| [Privaxy](https://github.com/Barre/privaxy) | HTTPS interception, URL filters and script/style injection | Selective interception with a locally trusted CA; implementation is native Go |
| [mitmproxy](https://docs.mitmproxy.org/stable/concepts/certificates/) | Per-install CA and configurable interception | Explicit trust, unique CA, leaving incompatible hosts opaque |
| [mitmproxy-adblock](https://github.com/dekoza/mitmproxy-adblock) | Ad filtering through an interception proxy | Demonstrates the approach, not reliable current YouTube support |
| [Invidious](https://docs.invidious.io/installation/) | Separate frontend and companion service | Alternative viewing route, not transparent official-app filtering |

The filter strips known ad metadata from player/next JSON and initial player
objects inside HTML scripts while retaining media URLs, captions, playability
status and unrelated fields. Only `youtube.com`, `www.youtube.com` and
`m.youtube.com` are intercepted; upstream TLS certificates are verified.

It does not remove ads stitched into media segments (SSAI), creator sponsorships
or arbitrary protobuf responses. YouTube experiments and anti-adblock behavior
can break metadata-based filtering. No broad Google interception certificate,
CDN-domain blocking or video transcoding is used.

## Standalone compatibility mode

The existing binary can still run a separate unmanaged filter:

```sh
/usr/local/x-ui/x-ui youtube-proxy -listen 127.0.0.1:18080 -ca-dir /etc/x-ui/youtube-filter
/usr/local/x-ui/x-ui youtube-proxy -print-routing xray
/usr/local/x-ui/x-ui youtube-proxy -print-routing singbox
```

Standalone mode is independent of panel profiles/pause. Its emitted fragments
must be merged and scoped manually. Remove its routes before stopping it. It
supports only loopback listeners and HTTPS CONNECT to port 443; reach it through
an authenticated VPN or SSH tunnel. Prefer managed mode for panel integration.

Verification:

```sh
go test -race ./internal/adblock/youtubeproxy
XUI_ADBLOCK_XRAY_BINARY=/path/to/xray \
XUI_ADBLOCK_SINGBOX_BINARY=/path/to/sing-box \
go test ./internal/web/service -run TestAdBlockManaged
```
