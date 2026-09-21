import test from 'node:test';
import assert from 'node:assert/strict';
import { createReaderController } from '../internal/web/static/js/reader.js';

function harness() {
  globalThis.localStorage = { getItem: () => null };
  globalThis.window = { scrollTo() {}, setTimeout, clearTimeout, requestAnimationFrame: cb => setTimeout(cb, 0) };
  const node = () => ({ innerHTML: '', textContent: '', classList: { add() {} }, setAttribute() {}, removeAttribute() {} });
  const el = Object.fromEntries(['readerContent', 'readerNovelTitle', 'readerChapterTitle', 'btnPrevChapter', 'btnNextChapter'].map(k => [k, node()]));
  const state = { tts: {}, chapters: [{chapterNo: 1, hasSource: true}, {chapterNo: 2, hasSource: true}] };
  const pending = [], bookmarks = [];
  const api = (path, options) => {
    if (path.endsWith('/bookmark')) { bookmarks.push(JSON.parse(options.body)); return Promise.resolve(); }
    return new Promise((resolve, reject) => pending.push({ resolve, reject, options }));
  };
  const controller = createReaderController({ state, el, api, showView: view => state.currentView = view,
    showToast() {}, maxChapterNo: () => 2, adjacentChapterNo: (n, d) => n + d >= 1 && n + d <= 2 ? n + d : null,
    openNovelDetail() {}, triggerQuickTranslate() {}, stopTTS() {} });
  return { state, el, pending, bookmarks, controller };
}
test('slow previous chapter cannot replace the chapter selected last', async () => {
  const h = harness();
  const first = h.controller.openChapter('book', 1);
  const second = h.controller.openChapter('book', 2);
  h.pending[1].resolve({ sourceTitle: 'second', sourceText: ['second'] }); await second;
  h.pending[0].resolve({ sourceTitle: 'first', sourceText: ['first'] }); await first;
  assert.equal(h.state.currentChapterData.sourceTitle, 'second');
  assert.deepEqual(h.bookmarks.map(x => x.chapterNo), [2]);
});
test('failed navigation clears previous data and does not save a false bookmark', async () => {
  const h = harness(); h.state.currentChapterData = {sourceText: ['old']};
  const request = h.controller.openChapter('book', 2);
  h.pending[0].reject(new Error('offline')); await request;
  assert.equal(h.state.currentChapterData, null);
  assert.equal(h.bookmarks.length, 0);
});
test('leaving reader prevents an outstanding response from being rendered', async () => {
  const h = harness(); const request = h.controller.openChapter('book', 1);
  h.state.currentView = 'library';
  h.pending[0].resolve({ sourceText: ['late response'] }); await request;
  assert.equal(h.state.currentChapterData, null);
  assert.equal(h.bookmarks.length, 0);
});
