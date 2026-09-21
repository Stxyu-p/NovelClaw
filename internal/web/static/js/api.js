export function createAPIClient({ onError } = {}) {
  return async function api(path, options = {}) {
    const { silent = false, headers = {}, timeoutMs = 180000, signal, ...fetchOptions } = options;
    const controller = new AbortController();
    const abort = () => controller.abort(signal?.reason);
    if (signal?.aborted) abort();
    else signal?.addEventListener('abort', abort, { once: true });
    let timedOut = false;
    const timer = setTimeout(() => { timedOut = true; controller.abort(); }, timeoutMs);
    try {
      const res = await fetch(path, {
        cache: 'no-store',
        ...fetchOptions,
        signal: controller.signal,
        headers: {
          'Content-Type': 'application/json',
          ...headers,
        },
      });
      if (!res.ok) {
        const payload = await res.json().catch(() => ({ error: res.statusText }));
        throw new Error(payload.error || `HTTP ${res.status}`);
      }
      if (res.status === 204) return null;
      return await res.json();
    } catch (err) {
      if (timedOut) err = new Error('การเชื่อมต่อใช้เวลานานเกินไป กรุณาลองอีกครั้ง');
      if (!silent && err.name !== 'AbortError' && typeof onError === 'function') onError(err);
      throw err;
    } finally {
      clearTimeout(timer);
      signal?.removeEventListener('abort', abort);
    }
  };
}
