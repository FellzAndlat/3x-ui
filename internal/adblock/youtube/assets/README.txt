3x-ui YouTube Companion (Chromium 111+, Chrome, Edge)

1. Unzip the downloaded archive.
2. Open chrome://extensions (Edge: edge://extensions).
3. Enable Developer mode, choose Load unpacked, select the unzipped folder
   containing manifest.json, then refresh the YouTube page.
4. Use the extension popup to enable/disable filtering.

No server URL, credentials, root certificate or panel login is required.
The extension only runs on www.youtube.com and m.youtube.com over HTTPS.
It does not upload browsing data, fetch remote code, or modify video URLs.
It removes known ad fields in initial/fetch player JSON and skips finite videos
that the page explicitly marks as ads. It hides selected promotional elements.

This implementation is experimental. YouTube player changes, server-side ad
insertion and anti-adblock checks can require updates; not all ads can be removed.
It does not work inside the official YouTube Android/iOS application. A VPN
client does not expose that application's encrypted player response to scripts.
