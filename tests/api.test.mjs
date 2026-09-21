import test from 'node:test';
import assert from 'node:assert/strict';
import { createAPIClient } from '../internal/web/static/js/api.js';
test('stalled requests time out and caller cancellation stays silent', async () => {
  globalThis.fetch = (_, {signal}) => new Promise((resolve, reject) => {
    const rejectAbort = () => reject(new DOMException('aborted', 'AbortError'));
    if (signal.aborted) rejectAbort(); else signal.addEventListener('abort', rejectAbort);
  });
  const errors = [];
  const api = createAPIClient({onError: error => errors.push(error.message)});
  await assert.rejects(api('/slow',{timeoutMs:5}), /นานเกินไป/);
  const controller = new AbortController(); controller.abort();
  await assert.rejects(api('/cancelled',{signal:controller.signal}),{name:'AbortError'});
  assert.equal(errors.length,1);
});
