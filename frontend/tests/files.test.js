import test from 'node:test';
import assert from 'node:assert/strict';
import { fileProblem, hasFiles, MAX_FILE } from '../src/files.js';

test('accepts supported book formats', () => {
  for (const name of ['book.txt', 'Война и мир.EPUB', 'scan.pdf']) assert.equal(fileProblem({ name, size: 10 }), '');
});

test('rejects unsupported, empty and oversized files', () => {
  assert.match(fileProblem({ name: 'photo.jpg', size: 10 }), /TXT, PDF и EPUB/);
  assert.match(fileProblem({ name: 'big.txt', size: MAX_FILE + 1 }), /12 МБ/);
  assert.match(fileProblem({ name: 'empty.txt', size: 0 }), /пустой/);
});

test('only drags with files activate the drop target', () => {
  assert.equal(hasFiles({ dataTransfer: { types: ['Files'] } }), true);
  assert.equal(hasFiles({ dataTransfer: { types: ['text/plain'] } }), false);
  assert.equal(hasFiles({}), false);
});
