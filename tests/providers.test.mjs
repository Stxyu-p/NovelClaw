import test from 'node:test';
import assert from 'node:assert/strict';
import { createProviderController } from '../internal/web/static/js/providers.js';

test('late model discovery cannot switch back to a previously selected provider', async () => {
  let complete;
  const state = {providers: [{id:'old'}], activeProvider:'old', translationProvider:'old', settingsProvider:'none', availableModels:['new-model']};
  const controller = createProviderController({state,el:{},api:()=>new Promise(resolve=>complete=resolve)});
  const pending = controller.discoverModels('old',{updateTranslation:true});
  state.translationProvider='new';
  complete({models:['old-model']}); await pending;
  assert.equal(state.translationProvider,'new');
  assert.deepEqual(state.availableModels,['new-model']);
});
