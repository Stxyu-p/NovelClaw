// Keep the captured chapter identity with the position; navigation must never
// turn a delayed scroll event into a bookmark for another chapter.
export function createReadingPosition({ state, api, host = window, storage = localStorage, doc = document }) {
  let timer = null;
  let pending = null;
  const writes = new Map();

  function saveBookmark(slug, chapterNo, scrollPercentage = 0, keepalive = false) {
    const previous = writes.get(slug) || Promise.resolve();
    const write = previous.catch(() => {}).then(() => api(`/api/novels/${encodeURIComponent(slug)}/bookmark`, {
      method: 'POST', silent: true, keepalive, timeoutMs: 10000,
      body: JSON.stringify({ chapterNo, scrollPercentage }),
    })).catch(error => console.warn('bookmark save failed', error));
    writes.set(slug, write);
    write.finally(() => { if (writes.get(slug) === write) writes.delete(slug); });
    return write;
  }

  function flush(keepalive = false) {
    clearTimeout(timer);
    if (!pending) return;
    const { slug, chapterNo, top, percentage } = pending;
    pending = null;
    try { storage.setItem(`nc_scroll_${slug}_${chapterNo}`, String(top)); } catch (error) { console.warn('local bookmark unavailable', error); }
    return saveBookmark(slug, chapterNo, percentage, keepalive);
  }

  function bind() {
    host.addEventListener('scroll', () => {
      if (state.currentView !== 'reader' || !state.currentChapterData || !state.currentSlug) return;
      const top = host.scrollY;
      const height = doc.documentElement.scrollHeight - doc.documentElement.clientHeight;
      pending = { slug: state.currentSlug, chapterNo: state.currentChapterNo, top,
        percentage: height > 0 ? Math.max(0, Math.min(100, top / height * 100)) : 0 };
      clearTimeout(timer);
      timer = setTimeout(flush, 1200);
    }, { passive: true });
    host.addEventListener('pagehide', () => flush(true));
    doc.addEventListener('visibilitychange', () => { if (doc.hidden) flush(true); });
  }
  return { bind, flush, saveBookmark };
}
