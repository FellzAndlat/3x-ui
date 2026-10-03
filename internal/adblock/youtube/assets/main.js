(() => {
  'use strict';
  const guard = Symbol.for('3x-ui.youtube.companion');
  if (window[guard]) return;
  window[guard] = true;
  let enabled = false;
  const adKeys = ['adPlacements', 'playerAds', 'adSlots', 'adBreakHeartbeatParams'];
  const hasAds = (value, depth = 0) => depth < 8 && value && typeof value === 'object' && !Array.isArray(value) && (adKeys.some((key) => Object.prototype.hasOwnProperty.call(value, key)) || hasAds(value.playerResponse, depth + 1));
  const clean = (value) => {
    if (!enabled || !value || typeof value !== 'object' || Array.isArray(value)) return value;
    for (const key of adKeys) {
      // Clone parsed responses/initial objects first; never touch media URLs,
      // live stream timing, captions, authentication, or unrelated JSON fields.
      if (Object.prototype.hasOwnProperty.call(value, key)) delete value[key];
    }
    if (value.playerResponse && typeof value.playerResponse === 'object') clean(value.playerResponse);
    return value;
  };
  const cleanCopy = (value) => {
    if (!enabled || !hasAds(value)) return value;
    try { return clean(JSON.parse(JSON.stringify(value))); } catch { return value; }
  };
  const isPlayerAPI = (input) => {
    try {
      const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url, location.href);
      return url.origin === location.origin && /^\/youtubei\/v\d+\/(player|next)$/.test(url.pathname);
    } catch { return false; }
  };
  const originalFetch = window.fetch;
  if (typeof originalFetch === 'function') {
    window.fetch = async function (...args) {
      const response = await Reflect.apply(originalFetch, this, args);
      if (!enabled || !isPlayerAPI(args[0]) || response.status !== 200 || !response.headers.get('content-type')?.includes('json')) return response;
      try {
        const text = await response.clone().text();
        if (text.length > 3 * 1024 * 1024) return response;
        const data = JSON.parse(text);
        if (!enabled || !hasAds(data)) return response;
        const changed = JSON.stringify(clean(data));
        if (changed === text) return response;
        const headers = new Headers(response.headers);
        headers.delete('content-length'); headers.delete('content-encoding');
        // Keep native fetch semantics for URL/redirect metadata. Override only
        // this response's body accessors, rather than returning a URL-less Response.
        const filtered = new Response(changed, {status: response.status, statusText: response.statusText, headers});
        const decorate = (copy) => {
          for (const key of ['url', 'redirected', 'type']) Object.defineProperty(copy, key, {value: response[key]});
          const clone = copy.clone.bind(copy);
          Object.defineProperty(copy, 'clone', {value: () => decorate(clone())});
          return copy;
        };
        return decorate(filtered);
      } catch { return response; }
    };
  }
  const installInitialHook = (key) => {
    const descriptor = Object.getOwnPropertyDescriptor(window, key);
    if (descriptor && (!descriptor.configurable || descriptor.writable === false || descriptor.get || descriptor.set)) return;
    let value = cleanCopy(descriptor?.value);
    Object.defineProperty(window, key, {configurable: true, enumerable: descriptor?.enumerable ?? true,
      get: () => value, set: (next) => {value = cleanCopy(next);}});
  };
  installInitialHook('ytInitialPlayerResponse');
  window.addEventListener('message', (event) => {
    if (event.source !== window || event.origin !== location.origin || event.data?.type !== '3x-ui-youtube-settings' || typeof event.data.enabled !== 'boolean') return;
    enabled = event.data.enabled;
    if (enabled) {
      const descriptor = Object.getOwnPropertyDescriptor(window, 'ytInitialPlayerResponse');
      if (descriptor?.set) window.ytInitialPlayerResponse = window.ytInitialPlayerResponse;
    }
  });
})();
