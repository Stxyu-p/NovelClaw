import test from 'node:test';
import assert from 'node:assert/strict';
import { createAudioCache } from '../internal/web/static/js/audio_cache.js';

test('1,000 spoken paragraphs retain at most three audio URLs', async () => {
  const live = new Set(); let peak = 0, sequence = 0;
  const cache = createAudioCache({
    fetchAudio: async () => ({ok: true, blob: async () => ({})}),
    urls: {createObjectURL() { const url = `blob:${++sequence}`; live.add(url); peak = Math.max(peak, live.size); return url; }, revokeObjectURL: url => live.delete(url)},
  });
  for (let i = 0; i < 1000; i++) await cache.get(String(i), {text: 'paragraph'});
  assert.equal(peak, 3); assert.equal(live.size, 3);
  cache.clear(); assert.equal(live.size, 0);
});
test('prefetch and playback share the same in-flight request', async () => {
  let count = 0;
  const cache = createAudioCache({ fetchAudio: async () => {count++; return {ok: true, blob: async () => ({})};},
    urls: {createObjectURL: () => 'blob:1', revokeObjectURL() {}} });
  assert.deepEqual(await Promise.all([cache.get('one', {}), cache.get('one', {})]), ['blob:1','blob:1']);
  assert.equal(count, 1);
});
test('stop during blob decoding does not allocate a late URL', async () => {
  let complete; let signal; let created = 0;
  const cache = createAudioCache({ fetchAudio: async (_, options) => { signal = options.signal; return {ok: true, blob: () => new Promise(resolve => complete = resolve)}; },
    urls: {createObjectURL: () => {created++;}, revokeObjectURL() {}} });
  const pending = cache.get('one', {});
  await Promise.resolve(); cache.clear(); complete({});
  await assert.rejects(pending, { name: 'AbortError' });
  assert.equal(signal.aborted, true); assert.equal(created, 0);
});
