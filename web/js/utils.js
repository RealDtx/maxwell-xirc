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
