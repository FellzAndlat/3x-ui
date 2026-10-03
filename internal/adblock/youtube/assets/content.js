(() => {
  'use strict';
  let enabled = false;
  let queued = false;
  let observer;
  let ticker;
  let lastSkipVideo;
  let lastSkipSource;
  let lastSkipTime = 0;
  const notify = () => window.postMessage({type: '3x-ui-youtube-settings', enabled}, location.origin);
  const step = () => {
    queued = false;
    if (!enabled) return;
    const player = document.querySelector('#movie_player, .html5-video-player');
    const video = player?.querySelector('video');
    if (!player || !video || !player.classList.contains('ad-showing')) {
      lastSkipVideo = undefined;lastSkipSource = undefined;
      return;
    }
    const skip = player.querySelector('.ytp-skip-ad-button, .ytp-ad-skip-button, .ytp-ad-skip-button-modern');
    if (skip && !skip.disabled && skip.getClientRects().length && Date.now() - lastSkipTime > 500) {
      lastSkipTime = Date.now();skip.click();return;
    }
    // Seek only a finite video explicitly marked as an ad. Never seek live
    // streams, ordinary videos or Shorts without that player marker.
    if (!video.paused && Number.isFinite(video.duration) && video.duration > 0 && video.duration < 600 && video.seekable.length && (lastSkipVideo !== video || lastSkipSource !== `${video.currentSrc}|${video.duration}`)) {
      try {video.currentTime = Math.max(0, video.duration - 0.05);lastSkipVideo = video;lastSkipSource = `${video.currentSrc}|${video.duration}`;} catch {}
    }
  };
  const schedule = () => {
    if (!enabled || queued) return;
    queued = true;requestAnimationFrame(step);
  };
  const apply = (value) => {
    enabled = value !== false;
    document.documentElement?.toggleAttribute('data-3x-ui-youtube', enabled);
    notify();observer?.disconnect();clearInterval(ticker);queued = false;
    if (enabled) {
      observer ??= new MutationObserver(schedule);
      observer.observe(document, {subtree: true, childList: true, attributes: true, attributeFilter: ['class']});
      ticker = setInterval(schedule, 1000);schedule();
    } else {lastSkipVideo = undefined;lastSkipSource = undefined;}
  };
  chrome.storage.local.get({enabled: true}, (settings) => apply(chrome.runtime.lastError ? false : settings.enabled));
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area === 'local' && changes.enabled) apply(changes.enabled.newValue);
  });
  document.addEventListener('DOMContentLoaded', () => {document.documentElement?.toggleAttribute('data-3x-ui-youtube', enabled);notify();schedule();}, {once: true});
  document.addEventListener('yt-navigate-finish', schedule);
  window.addEventListener('pagehide', () => {enabled = false;observer?.disconnect();clearInterval(ticker);});
})();
