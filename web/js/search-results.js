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

function normalizeSearchRow(row) {
    const r = row || {};
    return Object.assign({}, r, {
        size_display: toSizeDisplay(r.filesize),
    });
}

if (typeof module !== 'undefined' && module.exports) {
    module.exports = { formatBytesForSearch, toSizeDisplay, normalizeSearchRow };
}
