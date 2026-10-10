function formatBytesForSearch(bytes) {
    if (!Number.isFinite(bytes) || bytes <= 0) return '-';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
}

function toSizeDisplay(filesize) {
    if (filesize == null || filesize === '' || filesize === 0 || filesize === '0') return '-';
    if (typeof filesize === 'number') return formatBytesForSearch(filesize);
    if (typeof filesize === 'string') return filesize.trim() || '-';
    return '-';
}

// sizeToBytes parses bot-advertised sizes ("1.4G", "700M", "2.1 GB", "512K",
// plain byte counts) so search results sort numerically; unknown → -1.
function sizeToBytes(size) {
    if (typeof size === 'number') return size;
    const m = /^\s*([\d.,]+)\s*([KMGT]?)i?B?\s*$/i.exec(String(size || ''));
    if (!m) return -1;
    const n = parseFloat(m[1].replace(',', '.'));
    if (!Number.isFinite(n)) return -1;
    return n * Math.pow(1024, ' KMGT'.indexOf(m[2].toUpperCase() || ' '));
}

// searchSortValue returns the value to sort a search row by for column col.
function searchSortValue(row, col) {
    if (col === 'filesize') return sizeToBytes(row.filesize);
    if (col === 'first_seen') return Date.parse(rowFirstSeen(row)) || 0;
    if (col === 'last_seen') return Date.parse(rowLastSeen(row)) || 0;
    return row[col];
}

function normalizeSearchRow(row) {
    const r = row || {};
    return Object.assign({}, r, {
        size_display: toSizeDisplay(r.filesize),
    });
}

// Index rows carry first/last_seen_at; live search rows only created_at.
function rowFirstSeen(row) { return row.first_seen_at || row.created_at || null; }
function rowLastSeen(row) { return row.last_seen_at || row.created_at || null; }

function isoSeconds(iso) {
    var t = Date.parse(iso);
    return isNaN(t) ? '' : new Date(t).toISOString().replace(/\.\d{3}Z$/, 'Z');
}

// formatXircLink mirrors links.Format (Go): xirc://host/channel/bot/pack?…
function formatXircLink(row, host) {
    if (!host || !row.bot_nick || row.pack_number == null) return '';
    var q = new URLSearchParams();
    if (row.filename) q.set('name', row.filename);
    if (row.filesize != null && row.filesize !== '') q.set('size', String(row.filesize));
    var first = isoSeconds(rowFirstSeen(row)), last = isoSeconds(rowLastSeen(row));
    if (first) q.set('first', first);
    if (last) q.set('last', last);
    var s = 'xirc://' + host + '/' + encodeURIComponent(row.channel || '') + '/' +
        encodeURIComponent(row.bot_nick) + '/' + row.pack_number;
    var qs = q.toString();
    return qs ? s + '?' + qs : s;
}

if (typeof module !== 'undefined' && module.exports) {
    module.exports = { formatBytesForSearch, toSizeDisplay, normalizeSearchRow, sizeToBytes, searchSortValue, rowFirstSeen, rowLastSeen, formatXircLink };
}
