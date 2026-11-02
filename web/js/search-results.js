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

// parsePackSpec turns "5", "5,7,9" or "5-8" (mixable) into pack numbers, max 50.
function parsePackSpec(spec) {
    var packs = [];
    var parts = String(spec || '').split(',');
    for (var i = 0; i < parts.length; i++) {
        var p = parts[i].trim().replace(/^#/, '');
        if (!p) continue;
        var m = /^(\d+)\s*-\s*#?(\d+)$/.exec(p);
        var from = m ? +m[1] : +p, to = m ? +m[2] : +p;
        if (!Number.isInteger(from) || !Number.isInteger(to) || from < 1 || to < from) {
            return { packs: [], error: 'Invalid pack: ' + parts[i].trim() };
        }
        for (var n = from; n <= to; n++) {
            if (packs.indexOf(n) < 0) packs.push(n);
            if (packs.length > 50) return { packs: [], error: 'At most 50 packs at once' };
        }
    }
    return packs.length ? { packs: packs, error: '' } : { packs: [], error: 'Enter a pack number' };
}

// parseMsgCommands pulls "/msg <bot> xdcc send|get #<pack>" requests out of
// pasted text, one per line (the leading slash and "#" are optional).
function parseMsgCommands(text) {
    var out = [], bad = [];
    String(text || '').split(/\r?\n/).forEach(function(line) {
        line = line.trim();
        if (!line) return;
        var m = /^\/?msg\s+(\S+)\s+xdcc\s+(?:send|get)\s+#?(\d+)\s*$/i.exec(line);
        if (m && +m[2] > 0) out.push({ bot: m[1], pack: +m[2] });
        else bad.push(line);
    });
    return { cmds: out, bad: bad };
}

// parseIrcUrl parses irc://host[:port]/channel[,channel] (ircs:// = TLS).
// A bare "#" in the path is taken literally, as people paste it unencoded.
function parseIrcUrl(text) {
    var m = /^(ircs?):\/\/([^\/:\s]+)(?::(\d+))?\/?([^\s?]*)/i.exec(String(text || '').trim());
    if (!m) return null;
    var ssl = m[1].toLowerCase() === 'ircs';
    var chans = [];
    decodeURIComponent(m[4]).split(',').forEach(function(c) {
        c = c.trim();
        if (!c || /^(isnick|isserver|needpass|needkey)$/i.test(c)) return;
        if (!/^[#&+!]/.test(c)) c = '#' + c;
        if (chans.indexOf(c) < 0) chans.push(c);
    });
    return { host: m[2].toLowerCase(), port: m[3] ? +m[3] : (ssl ? 6697 : 6667), ssl: ssl, channels: chans };
}

if (typeof module !== 'undefined' && module.exports) {
    module.exports = { formatBytesForSearch, toSizeDisplay, normalizeSearchRow, sizeToBytes, searchSortValue, rowFirstSeen, rowLastSeen, formatXircLink, parsePackSpec, parseMsgCommands, parseIrcUrl };
}
