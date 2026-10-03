# FPTN, OpenFlux and Snell

The panel stores each protocol's own settings, credentials and subscription format. FPTN and OpenFlux are managed external processes; Snell is served by sing-box.

| Protocol | Incoming connections | Outgoing connections | Subscription |
| --- | --- | --- | --- |
| Snell | sing-box 1.14+, v5 or v6, individual user keys | Native sing-box v4 (v5-compatible) or v6 | `snell://` links; native sing-box JSON |
| FPTN | One isolated Docker server per inbound | Isolated FPTN CLI container and a loopback VLESS bridge for Xray or sing-box | Upstream `fptn:` access token with certificate fingerprint |
| OpenFlux | L4 exit process, one client per inbound | Loopback SOCKS5 bridge for Xray or sing-box, including UDP | Upstream `openflux://v1/` compressed link |

## Requirements and setup

Use Linux amd64 or arm64 for the managed FPTN components. Install Docker, make `/dev/net/tun` available, and ensure the panel service can run Docker. The default FPTN server image is `fptnvpn/fptn-vpn-server:0.4.4`; the image version can be changed in the settings JSON. Docker downloads the server image on first start. For FPTN outgoing connections, use **Install FPTN client** and install sing-box using the panel's existing core installer. The helper image uses the official CLI package 0.4.6, verified against the release SHA-256 digest, without installing its package scripts on the host.

Use **Install OpenFlux** before enabling an OpenFlux profile. The installer uses the official v0.3.0 release and verifies its SHA-256 digest. Components must also be installed on any assigned remote node; the install button operates on the panel hosting the UI.

Open the incoming connection form and choose the protocol. FPTN generates a certificate, metrics key and client secrets automatically. Its standard and mobile presets use MTU 1400 and 1280 respectively. Session limits, bandwidth, permitted SNI names and the upstream traffic filters remain configurable. Bandwidth 0 uses the largest supported upstream rate (2000 Mibit/s), because upstream does not implement an unlimited zero-rate bucket.

OpenFlux enables encrypted, negotiated batched sessions. The direct preset advertises the inbound's public address and port. Additional channel transports require their own HTTPS URLs. Each inbound needs a separate client and separate channel resources: upstream replaces the active session when another client joins. Stream-mode and unencrypted legacy links are rejected.

Snell generates a deployment PSK and individual user keys. The automatic preset selects v6. Server v5 uses the v4-compatible client setting; QUIC-only client v5 is not offered. Client applications must understand the selected version and individual user-key settings; a `snell://` URI alone does not imply support in every application.

## Routing and subscriptions

Import an upstream access link into an outgoing connection to preserve the protocol settings. Routes can select its tag through either core. The panel replaces the managed outbound with a local bridge when generating the runtime configuration and supervises the external process. FPTN's bridge binds traffic and DNS to `fptn0`; a failed tunnel has no direct-network fallback. Its default DNS uses IPv4.

FPTN and OpenFlux incoming profiles terminate outside the proxy core and use their own outgoing networking. Core inbound routing, sniffing, TLS/Reality options and core-specific filters do not apply to those profiles. Use FPTN's own filters where needed. Snell incoming profiles use normal sing-box routing.

The public address, node address and endpoint overrides are resolved by the server when exporting links and QR codes. Automatic FPTN certificate material is stored with the inbound so the advertised fingerprint matches the node's certificate. Credentials survive ordinary edits. Replacing a certificate or encryption key requires clients to refresh their profiles.

FPTN/OpenFlux connections cannot be represented by standard Xray, sing-box or Clash subscription JSON. Subscriptions containing them fall back to raw access links, preserving the connections instead of omitting them. Use the matching upstream client to import those links. Traffic is collected from FPTN's authenticated metrics endpoint or OpenFlux's private IPC socket and feeds the panel's existing client quota and expiry management.

## Validation

Backend compilation, frontend TypeScript checking and frontend production build are checked during implementation. Automated tests and live network handshakes are not part of this change's validation. Before deployment, verify TCP/UDP traffic, subscription refresh, quota enforcement, node operation and recovery after tunnel failure against real endpoints for each enabled protocol.
