const CHUNK_RELOAD_KEY = '3x-ui:chunk-reload';
const CHUNK_RELOAD_PARAM = '_3xui_chunk_reload';
const CHUNK_RELOAD_COOLDOWN = 10_000;

let chunkReloadScheduled = false;

function parseReloadTimestamp(value: string | null): number {
  const timestamp = Number(value ?? 0);
  return Number.isFinite(timestamp) ? timestamp : 0;
}

function getStoredReloadTimestamp(): number {
  try {
    return parseReloadTimestamp(sessionStorage.getItem(CHUNK_RELOAD_KEY));
  } catch {
    return 0;
  }
}

function clearChunkReloadParam() {
  const url = new URL(window.location.href);
  const reloadTimestamp = parseReloadTimestamp(url.searchParams.get(CHUNK_RELOAD_PARAM));
  if (!reloadTimestamp) return;

  // Keep the cache-busting parameter in the URL when sessionStorage is not
  // available. It then doubles as the cross-navigation reload-loop guard.
  try {
    sessionStorage.setItem(CHUNK_RELOAD_KEY, String(reloadTimestamp));
    url.searchParams.delete(CHUNK_RELOAD_PARAM);
    window.history.replaceState(window.history.state, '', url.toString());
  } catch {
    // The URL timestamp remains available as a fallback loop guard.
  }
}

export function isChunkLoadError(reason: unknown): boolean {
  const message = reason instanceof Error ? reason.message : String(reason ?? '');
  return /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module/i.test(
    message,
  );
}

export function reloadAfterChunkLoadError(): boolean {
  // Vite can emit vite:preloadError and then reject the same dynamic import.
  // Treat the second event as handled while the forced navigation is pending.
  if (chunkReloadScheduled) return true;

  const now = Date.now();
  const url = new URL(window.location.href);
  const urlReloadTimestamp = parseReloadTimestamp(url.searchParams.get(CHUNK_RELOAD_PARAM));
  const lastReload = Math.max(urlReloadTimestamp, getStoredReloadTimestamp());

  // If the freshly loaded page still cannot load its chunk, do not create an
  // automatic reload loop. Let the original error surface instead.
  if (now - lastReload < CHUNK_RELOAD_COOLDOWN) return false;

  chunkReloadScheduled = true;

  try {
    sessionStorage.setItem(CHUNK_RELOAD_KEY, String(now));
  } catch {
    // sessionStorage may be blocked; the URL parameter below is sufficient.
  }

  // A plain location.reload() can reuse stale HTML from an intermediary cache.
  // A unique query value forces a new document request while preserving the
  // current SPA route, existing query parameters, and hash.
  url.searchParams.set(CHUNK_RELOAD_PARAM, String(now));
  window.location.replace(url.toString());
  return true;
}

export function installChunkLoadRecovery() {
  clearChunkReloadParam();

  window.addEventListener('vite:preloadError', (event) => {
    if (reloadAfterChunkLoadError()) {
      event.preventDefault();
    }
  });

  window.addEventListener('unhandledrejection', (event) => {
    if (isChunkLoadError(event.reason) && reloadAfterChunkLoadError()) {
      event.preventDefault();
    }
  });
}

export async function importWithChunkRecovery<T>(loader: () => Promise<T>): Promise<T> {
  try {
    return await loader();
  } catch (error) {
    if (isChunkLoadError(error) && reloadAfterChunkLoadError()) {
      // Navigation has already been scheduled. Keep React.lazy suspended until
      // the new document replaces this stale application instance.
      return new Promise<T>(() => undefined);
    }
    throw error;
  }
}
