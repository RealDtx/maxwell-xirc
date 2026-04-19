const test = require('node:test');
const assert = require('node:assert/strict');
const { normalizeSearchRow } = require('./search-results.js');

test('normalizeSearchRow formats numeric filesize as human-readable', () => {
    const row = normalizeSearchRow({ filesize: 1048576 });
    assert.equal(row.size_display, '1.0 MB');
});

test('normalizeSearchRow preserves human-readable filesize strings', () => {
    const row = normalizeSearchRow({ filesize: '204M' });
    assert.equal(row.size_display, '204M');
});

test('normalizeSearchRow uses dash fallback for empty/zero/missing filesize', () => {
    assert.equal(normalizeSearchRow({ filesize: 0 }).size_display, '-');
    assert.equal(normalizeSearchRow({ filesize: '' }).size_display, '-');
    assert.equal(normalizeSearchRow({}).size_display, '-');
});
