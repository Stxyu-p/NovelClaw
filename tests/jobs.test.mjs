import test from 'node:test';
import assert from 'node:assert/strict';
import { createJobController } from '../internal/web/static/js/jobs.js';

function harness(jobs, currentJobId=null) {
 let source;
 globalThis.EventSource=class {constructor(){source=this}};
 globalThis.window={addEventListener(){}};
 const state={currentJobId,activeJobQueue:[]};
 const el=new Proxy({}, {get:(target,key)=>target[key]??=({style:{},classList:{add(){},remove(){},contains:()=>true}})});
 const controller=createJobController({state,el,api:typeof jobs === 'function' ? jobs : async()=>({jobs}),showToast(){},loadNovels(){},loadChapters(){}});
 controller.initSSE();
 return {state, send:data=>source.onmessage({data:JSON.stringify(data)})};
}
test('SSE reconnect restores an active job without periodic polling',async()=>{
 const h=harness([{jobId:'job_restored',status:'running',percentage:45,message:'working'}]);
 h.send({type:'connected'}); await new Promise(resolve=>setImmediate(resolve));
 assert.equal(h.state.currentJobId,'job_restored');
});
test('SSE reconnect clears a job that finished while disconnected',async()=>{
 const h=harness([],'job_finished');
 h.send({type:'connected'}); await new Promise(resolve=>setImmediate(resolve));
 assert.equal(h.state.currentJobId,null);
});
test('another job finishing cannot clear the job currently displayed',async()=>{
 const h=harness([],'job_current');
 h.send({jobId:'job_other',status:'completed'});
 assert.equal(h.state.currentJobId,'job_current');
});
test('another import finishing cannot clear the job currently displayed',()=>{
 const h=harness([],'job_current');
 for (const type of ['import_done','import_partial','import_cancelled','import_error']) {
  h.send({jobId:'import_other',type});
  assert.equal(h.state.currentJobId,'job_current');
 }
});
test('late reconnect snapshot cannot resurrect a completed job',async()=>{
 const pending=[];
 const h=harness(()=>new Promise(resolve=>pending.push(resolve)));
 h.send({type:'connected'});
 h.send({jobId:'job_finished',status:'completed'});
 pending[1]({jobs:[]});await new Promise(resolve=>setImmediate(resolve));
 pending[0]({jobs:[{jobId:'job_finished',status:'running',percentage:20}]});
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(h.state.currentJobId,null);
});
