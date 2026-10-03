// Opt-in live playback QA in a fresh browser, without signing into an account.
// Requires a reachable local filter (or SSH forward) and its public CA file.
import { readFileSync, writeFileSync } from 'node:fs';
import { createHash, X509Certificate } from 'node:crypto';
import http from 'node:http';
import tls from 'node:tls';
import { chromium } from 'playwright';

const proxyURL = new URL(process.env.YOUTUBE_QA_PROXY || 'http://127.0.0.1:18080');
if (
  proxyURL.protocol !== 'http:' ||
  !['127.0.0.1', '[::1]', 'localhost'].includes(proxyURL.hostname)
)
  throw new Error('Use a loopback proxy or SSH forwarding');
if (!process.env.YOUTUBE_QA_CA) throw new Error('Set YOUTUBE_QA_CA to the public CA file');
const ca = readFileSync(process.env.YOUTUBE_QA_CA);
async function trustedSPKI(host) {
  return new Promise((resolve, reject) => {
    const request = http.request({
      hostname: proxyURL.hostname.replace(/^\[|\]$/g, ''),
      port: proxyURL.port || 80,
      method: 'CONNECT',
      path: `${host}:443`,
      headers: { Host: `${host}:443` },
    });
    request.setTimeout(15000, () => request.destroy(new Error('CONNECT timeout')));
    request.on('error', reject);
    request.on('connect', (response, socket, head) => {
      if (response.statusCode !== 200) {
        socket.destroy();
        reject(new Error(`CONNECT ${response.statusCode}`));
        return;
      }
      if (head.length) socket.unshift(head);
      const secure = tls.connect({ socket, servername: host, ca, rejectUnauthorized: true });
      secure.setTimeout(15000, () => secure.destroy(new Error('TLS timeout')));
      secure.once('error', reject);
      secure.once('secureConnect', () => {
        const cert = new X509Certificate(secure.getPeerCertificate().raw);
        const hash = createHash('sha256')
          .update(cert.publicKey.export({ format: 'der', type: 'spki' }))
          .digest('base64');
        secure.destroy();
        resolve(hash);
      });
    });
    request.end();
  });
}
const hashes = await Promise.all(
  ['youtube.com', 'www.youtube.com', 'm.youtube.com'].map(trustedSPKI),
);
const scenarios = [
  ['video', process.env.YOUTUBE_QA_VIDEO],
  ['shorts', process.env.YOUTUBE_QA_SHORT],
  ['live', process.env.YOUTUBE_QA_LIVE],
];
if (scenarios.some(([, id]) => !id || !/^[-\w]{11}$/.test(id)))
  throw new Error(
    'Supply 11-character video IDs in YOUTUBE_QA_VIDEO, YOUTUBE_QA_SHORT and YOUTUBE_QA_LIVE; choose a video with captions',
  );
const browser = await chromium.launch({
  headless: process.env.YOUTUBE_QA_HEADED !== '1',
  args: [`--ignore-certificate-errors-spki-list=${hashes.join(',')}`],
});
const context = await browser.newContext({ proxy: { server: proxyURL.origin }, locale: 'en-US' });
const seconds = Number(process.env.YOUTUBE_QA_SECONDS || 30);
if (!Number.isInteger(seconds) || seconds < 5 || seconds > 180)
  throw new Error('YOUTUBE_QA_SECONDS must be 5–180');
const results = [];
try {
  for (const [kind, id] of scenarios) {
    const page = await context.newPage();
    await page.addInitScript(() => {
      window.__youtubeQAAdSeen = false;
      setInterval(() => {
        if (document.querySelector('.ad-showing')) window.__youtubeQAAdSeen = true;
      }, 250);
    });
    let markedAd = false;
    let adDataSeen = false;
    page.on('response', async (response) => {
      if (/\/youtubei\/v\d+\/player/.test(response.url())) {
        try {
          const body = await response.json();
          if (body.adPlacements || body.playerAds || body.adSlots) adDataSeen = true;
        } catch {}
      }
    });
    try {
      const url =
        kind === 'shorts'
          ? `https://www.youtube.com/shorts/${id}`
          : `https://www.youtube.com/watch?v=${id}`;
      const response = await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 60000 });
      if (!response?.ok()) throw new Error(`Page response ${response?.status()}`);
      const consent = page.getByRole('button', { name: /Reject all/i });
      if (await consent.count()) await consent.first().click({ timeout: 5000 });
      await page.locator('video').first().waitFor({ timeout: 30000 });
      await page
        .locator('video')
        .first()
        .evaluate((video) => {
          video.muted = true;
          return video.play();
        });
      const initial = await page
        .locator('video')
        .first()
        .evaluate((video) => video.currentTime);
      await page.waitForFunction(
        (start) => {
          const video = document.querySelector('video');
          return video && video.currentTime > start + 3;
        },
        initial,
        { timeout: 30000 },
      );
      await page.waitForTimeout(seconds * 1000);
      markedAd = await page.evaluate(
        () => window.__youtubeQAAdSeen === true || !!document.querySelector('.ad-showing'),
      );
      let seek = 'not-applicable';
      let captions = 'not-applicable';
      if (kind === 'video') {
        const target = await page
          .locator('video')
          .first()
          .evaluate((video) => {
            if (!Number.isFinite(video.duration) || video.duration < 20)
              throw new Error('Choose a finite video longer than 20 seconds');
            const target = Math.min(30, video.duration / 2);
            video.currentTime = target;
            return target;
          });
        await page.waitForFunction(
          (target) => {
            const video = document.querySelector('video');
            return video && !video.seeking && Math.abs(video.currentTime - target) < 8;
          },
          target,
          { timeout: 15000 },
        );
        seek = 'passed';
        const button = page.locator('.ytp-subtitles-button').first();
        if ((await button.count()) && (await button.getAttribute('aria-disabled')) !== 'true') {
          await button.click();
          await page.locator('.ytp-caption-segment').first().waitFor({ timeout: 30000 });
          captions = 'passed';
        } else {
          captions = 'unavailable';
        }
      }
      results.push({ kind, playback: 'passed', seek, captions, markedAd, adDataSeen });
    } catch (error) {
      results.push({ kind, playback: 'failed', error: String(error), markedAd, adDataSeen });
    } finally {
      await page.close();
    }
  }
} finally {
  await browser.close();
}
writeFileSync(
  process.env.YOUTUBE_QA_REPORT || 'youtube-live-check.json',
  JSON.stringify(
    {
      checkedAt: new Date().toISOString(),
      results,
      scope: 'Live playback QA; not proof that all ads were removed',
    },
    null,
    2,
  ),
);
console.log(JSON.stringify(results, null, 2));
if (
  results.some(
    (result) =>
      result.playback !== 'passed' ||
      result.markedAd ||
      result.adDataSeen ||
      (result.kind === 'video' && result.captions !== 'passed'),
  )
)
  process.exitCode = 1;
