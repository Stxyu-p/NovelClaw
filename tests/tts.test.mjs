import test from 'node:test';
import assert from 'node:assert/strict';
import { createTTSController } from '../internal/web/static/js/tts.js';

test('stopping while neural speech fails never starts fallback speech', async () => {
  let fail; let spoken = 0;
  globalThis.window = {speechSynthesis: {cancel() {}, speak() {spoken++;}}};
  globalThis.SpeechSynthesisUtterance = class {};
  globalThis.localStorage = {setItem() {}};
  globalThis.fetch = () => new Promise((_, reject) => fail = reject);
  const callbacks = {};
  const el = {btnReaderTTS: {addEventListener: (_, callback) => callbacks.start = callback},
    readerContent: {querySelectorAll: () => [], querySelector: () => null}};
  const state = {currentSlug: 'book', currentChapterNo: 1, currentChapterData: {sourceText: ['one paragraph']},
    tts: {voice: 'neural', speed: 1}};
  const controller = createTTSController({state, el, showToast() {}, adjacentChapterNo: () => null, openChapter() {}});
  controller.initTTS(); callbacks.start(); controller.stopTTS(); fail(new Error('offline'));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(spoken, 0); assert.equal(state.tts.speaking, false);
  assert.deepEqual(state.tts.paragraphs, []);
});
