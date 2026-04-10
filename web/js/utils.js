function formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
}

function formatSpeed(bytesPerSec) {
    if (!bytesPerSec) return '-';
    return formatSize(bytesPerSec) + '/s';
}

function formatETA(bytesRemaining, speed) {
    if (!speed || speed <= 0) return '-';
    const seconds = Math.ceil(bytesRemaining / speed);
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    if (h > 0) return `${h}:${String(m).padStart(2,'0')}:${String(s).padStart(2,'0')}`;
    return `${m}:${String(s).padStart(2,'0')}`;
}

function formatTime(isoString) {
    if (!isoString) return '-';
    const d = new Date(isoString);
    return d.toLocaleTimeString();
}

function formatDate(isoString) {
    if (!isoString) return '-';
    const d = new Date(isoString);
    return d.toLocaleDateString() + ' ' + d.toLocaleTimeString();
}
