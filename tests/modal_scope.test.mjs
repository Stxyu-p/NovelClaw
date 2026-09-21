import test from 'node:test';
import assert from 'node:assert/strict';
import { createGlossaryController } from '../internal/web/static/js/glossary.js';

test('late glossary cannot populate another novel or enable saving its old terms', async () => {
 const pending=[];
 const state={currentSlug:'first',glossaryTerms:[{term:'old',target:'old'}]};
 const el={modalGlossary:{classList:{contains:()=>false}},btnSaveGlossary:{},discStatus:{},glossaryTbody:{},glossaryQaResults:{}};
 const controller=createGlossaryController({state,el,api:()=>new Promise(resolve=>pending.push(resolve)),openModal(){},showToast(){},closeModal(){}});
 const first=controller.openGlossary();state.currentSlug='second';const second=controller.openGlossary();
 pending[0]({terms:[{term:'first',target:'wrong book'}]});await first;
 assert.deepEqual(state.glossaryTerms,[]);assert.equal(el.btnSaveGlossary.disabled,true);
 pending[1]({terms:[{term:'second',target:'correct'}]});await second;
 assert.equal(state.glossaryTerms[0].term,'second');assert.equal(el.btnSaveGlossary.disabled,false);
});
