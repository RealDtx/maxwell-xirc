/**
 * IRC color/formatting → HTML converter.
 * Converts IRC control codes to spans with CSS classes.
 */
var ircColors = (function() {
    // IRC color code: \x03FG[,BG] — up to 2 digits each
    var COLOR_RE = /\x03(\d{1,2})(?:,(\d{1,2}))?/g;
    var BOLD = '\x02';
    var ITALIC = '\x1D';
    var UNDERLINE = '\x1F';
    var STRIKETHROUGH = '\x1E';
    var REVERSE = '\x16';
    var RESET = '\x0F';

    function escapeHtml(str) {
        return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    }

    function toHtml(text) {
        if (!text) return '';
        var result = '';
        var openSpans = 0;
        var bold = false, italic = false, underline = false, strikethrough = false;
        var fg = null, bg = null;

        for (var i = 0; i < text.length; i++) {
            var ch = text[i];
            var code = text.charCodeAt(i);

            if (code === 0x03) {
                // Color code — parse digits
                var match = /^(\d{1,2})(?:,(\d{1,2}))?/.exec(text.substring(i + 1));
                if (match) {
                    // Close previous color span if any
                    if (fg !== null || bg !== null) {
                        result += '</span>';
                        openSpans--;
                    }
                    fg = parseInt(match[1], 10);
                    bg = match[2] !== undefined ? parseInt(match[2], 10) : null;
                    var cls = 'irc-fg-' + fg;
                    if (bg !== null) cls += ' irc-bg-' + bg;
                    result += '<span class="' + cls + '">';
                    openSpans++;
                    i += match[0].length; // skip the digits
                } else {
                    // \x03 with no digits = reset colors
                    if (fg !== null || bg !== null) {
                        result += '</span>';
                        openSpans--;
                    }
                    fg = null;
                    bg = null;
                }
            } else if (code === 0x02) {
                if (bold) { result += '</b>'; bold = false; }
                else { result += '<b>'; bold = true; }
            } else if (code === 0x1D) {
                if (italic) { result += '</i>'; italic = false; }
                else { result += '<i>'; italic = true; }
            } else if (code === 0x1F) {
                if (underline) { result += '</u>'; underline = false; }
                else { result += '<u>'; underline = true; }
            } else if (code === 0x1E) {
                if (strikethrough) { result += '</s>'; strikethrough = false; }
                else { result += '<s>'; strikethrough = true; }
            } else if (code === 0x16) {
                // Reverse video — swap fg/bg (simplified: just toggle a class)
                // Skip for now, complex to implement
            } else if (code === 0x0F) {
                // Reset all formatting
                if (bold) { result += '</b>'; bold = false; }
                if (italic) { result += '</i>'; italic = false; }
                if (underline) { result += '</u>'; underline = false; }
                if (strikethrough) { result += '</s>'; strikethrough = false; }
                while (openSpans > 0) { result += '</span>'; openSpans--; }
                fg = null; bg = null;
            } else {
                result += escapeHtml(ch);
            }
        }

        // Close any remaining open tags
        if (bold) result += '</b>';
        if (italic) result += '</i>';
        if (underline) result += '</u>';
        if (strikethrough) result += '</s>';
        while (openSpans > 0) { result += '</span>'; openSpans--; }

        return result;
    }

    return { toHtml: toHtml };
})();
