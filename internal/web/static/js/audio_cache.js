// One foreground paragraph plus one look-ahead. URLs are owned here so every
// eviction and cancellation has a matching revokeObjectURL.
export function createAudioCache({ fetchAudio = fetch, urls = URL, limit = 3 } = {}) {
  const entries = new Map();
  let generation = 0;
  function drop(key) {
    const entry = entries.get(key);
    if (!entry) return;
    entries.delete(key);
    entry.controller.abort();
    if (entry.url) urls.revokeObjectURL(entry.url);
  }
  function clear() { generation += 1; for (const key of entries.keys()) drop(key); }
  function get(key, payload) {
    const existing = entries.get(key);
    if (existing) { entries.delete(key); entries.set(key, existing); return existing.promise; }
    const version = generation;
    const entry = { controller: new AbortController(), url: null };
    entries.set(key, entry);
    while (entries.size > limit) drop(entries.keys().next().value);
    entry.promise = (async () => {
      try {
        const response = await fetchAudio('/api/audio/speech', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload), signal: entry.controller.signal,
        });
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const blob = await response.blob();
        if (version !== generation || entries.get(key) !== entry || entry.controller.signal.aborted) {
          throw new DOMException('Audio request superseded', 'AbortError');
        }
        entry.url = urls.createObjectURL(blob);
        return entry.url;
      } catch (error) { if (entries.get(key) === entry) drop(key); throw error; }
    })();
    return entry.promise;
  }
  return { get, clear };
}
