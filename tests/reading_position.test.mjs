import test from 'node:test';
import assert from 'node:assert/strict';
import { createReadingPosition } from '../internal/web/static/js/reading_position.js';

test('navigation flush saves the captured chapter, not the new state', async () => {
  const events = {}, saved = [], requests = [];
  const state = {currentView: 'reader', currentSlug: 'book', currentChapterNo: 1, currentChapterData: {}};
  const tracker = createReadingPosition({ state,
    host: {scrollY: 250, addEventListener: (event, cb) => events[event] = cb},
    doc: {documentElement: {scrollHeight: 1000, clientHeight: 500}, addEventListener() {}},
    storage: {setItem: (...args) => saved.push(args)},
    api: async (path, options) => requests.push([path, JSON.parse(options.body)]),
  });
  tracker.bind(); events.scroll(); state.currentChapterNo = 2;
  await tracker.flush();
  assert.deepEqual(saved, [['nc_scroll_book_1', '250']]);
  assert.deepEqual(requests[0][1], {chapterNo: 1, scrollPercentage: 50});
});
test('bookmark writes for a novel are serialized', async () => {
  const calls = []; let complete;
  const tracker = createReadingPosition({state: {}, host: {}, doc: {}, storage: {},
    api: (path, options) => {calls.push(JSON.parse(options.body).chapterNo); return calls.length === 1 ? new Promise(resolve => complete = resolve) : Promise.resolve();} });
  const first = tracker.saveBookmark('book', 1);
  const second = tracker.saveBookmark('book', 2);
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(calls, [1]); complete(); await Promise.all([first, second]);
  assert.deepEqual(calls, [1, 2]);
});
