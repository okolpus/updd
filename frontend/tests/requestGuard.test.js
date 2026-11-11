import test from 'node:test';
import assert from 'node:assert/strict';
import { createRequestGuard } from '../src/requestGuard.js';

function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
}

test('an old document response cannot enter another authenticated session', async () => {
  const guard = createRequestGuard();
  const responseA = deferred();
  let ui = null;
  const oldRequest = guard.runDocument(0, 0, () => responseA.promise, data => { ui = data; });

  // Logout invalidates all pending document consumers before its network call.
  guard.sessionEpoch.current++;
  guard.documentEpoch.current++;
  // A different account logs in and starts its own document request.
  guard.sessionEpoch.current++;
  guard.documentEpoch.current++;
  const currentRequest = guard.runDocument(2, 2, () => Promise.resolve('private document B'), data => { ui = data; });
  assert.equal(await currentRequest, true);
  responseA.resolve('private document A');
  assert.equal(await oldRequest, false);
  assert.equal(ui, 'private document B');
});

test('notes and questions from an old document cannot change the new document', async () => {
  const guard = createRequestGuard();
  const oldQuestion = deferred();
  const questions = [];
  const pending = guard.runDocument(0, 0, () => oldQuestion.promise, data => questions.push(data));
  guard.documentEpoch.current++;
  await guard.runDocument(0, 1, () => Promise.resolve('answer B'), data => questions.push(data));
  oldQuestion.resolve('answer A');
  assert.equal(await pending, false);
  assert.deepEqual(questions, ['answer B']);
});

test('a response for the current session and document applies exactly once', async () => {
  const guard = createRequestGuard();
  const response = deferred();
  const applied = [];
  const pending = guard.runDocument(0, 0, () => response.promise, value => applied.push(value));
  assert.deepEqual(applied, []);
  response.resolve({ id: 'current' });
  assert.equal(await pending, true);
  assert.deepEqual(applied, [{ id: 'current' }]);
});
