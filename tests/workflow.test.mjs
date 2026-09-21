import test from 'node:test';
import assert from 'node:assert/strict';
import { createWorkflowController } from '../internal/web/static/js/workflow.js';

test('retry translation retains the requested novel when another novel is open', async () => {
  globalThis.localStorage = {getItem: () => null, setItem() {}};
  const element = () => ({value: '', classList: {add() {}, remove() {}}, style: {}, checked: false});
  const el = Object.fromEntries(['transStart','transEnd','transProgressBox','transErrorMsg','transModelSelect','transGenre',
    'transForce','topProgressBar','floatingJobBar','floatJobTitle','floatJobPct','floatJobBar'].map(key => [key, element()]));
  el.transModelSelect.value = 'test-model';
  let submit, payload;
  el.formTranslate = {addEventListener: (_, cb) => submit = cb};
  el.transGenre.value = 'romance';
  const state = {currentSlug: 'different-book', activeProvider: 'custom', availableModels: [],
    novels: [{slug: 'original-book', genre: 'fantasy'}]};
  const controller = createWorkflowController({state, el, showToast() {}, openModal() {}, closeModal() {}, beginJob() {},
    api: async (_, options) => {payload = JSON.parse(options.body); return {};}});
  controller.bindWorkflowEvents(); controller.triggerQuickTranslate('original-book', 2, 2);
  await submit({preventDefault() {}});
  assert.equal(payload.novelSlug, 'original-book');
  assert.equal(payload.genre, 'fantasy');
});
