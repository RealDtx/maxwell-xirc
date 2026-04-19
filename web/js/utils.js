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

function formatDateCustom(isoString, fmt) {
    if (!isoString) return '-';
    var d = new Date(isoString);
    if (isNaN(d.getTime())) return '-';
    var DD = String(d.getDate()).padStart(2, '0');
    var MM = String(d.getMonth() + 1).padStart(2, '0');
    var YYYY = String(d.getFullYear());
    var HH = String(d.getHours()).padStart(2, '0');
    var mm = String(d.getMinutes()).padStart(2, '0');
    var SS = String(d.getSeconds()).padStart(2, '0');
    // Replace date tokens first, then time tokens.
    // Use intermediate placeholders to avoid double-replacement of MM.
    return fmt
        .replace('YYYY', YYYY)
        .replace('DD', DD)
        .replace(/MM/, MM)       // first MM = month
        .replace('HH', HH)
        .replace(/MM/, mm)       // second MM = minutes
        .replace('SS', SS);
}

function formatRelativeTime(isoString) {
    if (!isoString) return '-';
    var d = new Date(isoString);
    if (isNaN(d.getTime())) return '-';
    var diffMs = Date.now() - d.getTime();
    if (diffMs < 0) return 'just now';
    var sec = Math.floor(diffMs / 1000);
    if (sec < 60) return sec + 's ago';
    var min = Math.floor(sec / 60);
    if (min < 60) return min + 'm ago';
    var hr = Math.floor(min / 60);
    if (hr < 24) return hr + 'h ago';
    return null; // caller should fall back to full date
}

function formatDuration(startIso, endIso) {
    if (!startIso || !endIso) return '-';
    var ms = new Date(endIso).getTime() - new Date(startIso).getTime();
    if (ms < 0 || isNaN(ms)) return '-';
    var sec = Math.floor(ms / 1000);
    if (sec < 60) return sec + 's';
    var min = Math.floor(sec / 60);
    var s = sec % 60;
    if (min < 60) return min + 'm ' + s + 's';
    var hr = Math.floor(min / 60);
    var m = min % 60;
    if (hr < 24) return hr + 'h ' + m + 'm';
    var d = Math.floor(hr / 24);
    var h = hr % 24;
    return d + 'd ' + h + 'h';
}

// Format a raw IRC line into a human-readable system message.
// Strips the :prefix and converts common commands to plain text.
function formatRawLine(line) {
    if (!line) return '';
    // Strip leading :
    const parts = line.replace(/^\:/, '').split(' ');
    if (parts.length < 2) return line;

    // Extract nick from prefix (nick!user@host)
    const prefix = parts[0];
    const nick = prefix.includes('!') ? prefix.split('!')[0] : prefix;
    const cmd = parts[1] ? parts[1].toUpperCase() : '';

    // Trailing is the last colon-prefixed parameter
    const trailingIdx = parts.findIndex((p, i) => i >= 2 && p.startsWith(':'));
    const trailing = trailingIdx >= 0 ? parts.slice(trailingIdx).join(' ').replace(/^\:/, '') : '';

    switch (cmd) {
        case 'JOIN':   return `* ${nick} has joined`;
        case 'PART':   return `* ${nick} has left${trailing ? ' (' + trailing + ')' : ''}`;
        case 'QUIT':   return `* ${nick} has quit${trailing ? ' (' + trailing + ')' : ''}`;
        case 'KICK': {
            const target = parts[3] || '';
            return `* ${nick} kicked ${target}${trailing ? ' (' + trailing + ')' : ''}`;
        }
        case 'MODE': {
            const modeStr = parts.slice(3).join(' ').replace(/^\:/, '');
            return `* ${nick} sets mode ${modeStr}`;
        }
        case 'TOPIC': return `* ${nick} changed topic: ${trailing}`;
        case '332':   return `* Topic: ${trailing}`;
        case '333':   return '';  // topic setter timestamp — skip
        case '353':   return '';  // NAMES list — skip (too noisy)
        case '366':   return '';  // End of NAMES — skip
        default:      return trailing || line;
    }
}
