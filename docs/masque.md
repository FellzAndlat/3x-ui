# MASQUE (sing-box 1.15+)

MASQUE inbounds use the native `masque-server` CONNECT-IP endpoint. MASQUE is
available in the inbound picker only when sing-box is selected and its installed
version is 1.15+; prereleases must be 1.15.0-alpha.7 or newer.

The form automatically selects a free port, enables HTTP/3 with HTTP/2 and
HTTP/1.1 compatibility, supplies the standard URI template, IPv4/IPv6 tunnel
prefixes and MTU 1280, and uses the internal network stack. It requires no system
TUN device or additional process. Client usernames are their panel emails;
passwords use the ordinary client credential lifecycle. Empty certificate/key
paths select the panel HTTPS certificate, then the subscription HTTPS certificate.
If neither exists, configure a certificate pair explicitly.

The automatic settings button resets tunnel parameters without changing clients
or TLS credentials. Advertised routes may be left empty to allow all destinations.
Custom prefixes and routes are validated on the server. HTTP/3 binds UDP; HTTP/1.1
and HTTP/2 bind TCP on the same port. Port conflicts respect selected versions.

Use a **sing-box JSON subscription** with a client based on a MASQUE-capable
sing-box core. The profile contains `masque-client` under `endpoints` and routes
to its tag. No proprietary `masque://` share URI is invented. Xray and Clash
subscription formats cannot represent this endpoint. Import support in other
applications depends on their own core version and endpoint importer.

Native sing-box editor settings cannot overwrite generated MASQUE endpoints.
Disabling clients or deleting the inbound updates the native runtime through the
same restart path used by the other sing-box protocols.

Validation:

```sh
go test ./internal/masque ./internal/singbox ./internal/web/service ./internal/sub
SINGBOX_TEST_BINARY=/path/to/sing-box go test ./internal/singbox -run MASQUE
cd frontend
npm run typecheck
npm run test -- src/test/masque.test.ts
npm run build:ci
```

Reference: https://sing-box.sagernet.org/configuration/endpoint/masque-server/
