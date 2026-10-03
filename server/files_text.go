package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/RealDtx/maxwell-irc/fscheck"
)

// /api/files/text — view (all users) and edit (admins) text files below the
// configured roots. Files come from untrusted bots: binaries are refused by
// content, legacy encodings round-trip byte-exactly, writes are atomic.
const (
	textEditMax = 2 << 20   // larger files are read-only
	textWindow  = 256 << 10 // page size for read-only files
	sniffLen    = 8 << 10
)

type legacyEncoding struct {
	cm    *charmap.Charmap
	label string
}

var legacyEncodings = map[string]legacyEncoding{
	"cp437":        {charmap.CodePage437, "CP437"},
	"windows-1252": {charmap.Windows1252, "Windows-1252"},
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

type textFileResponse struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Mtime      time.Time `json:"mtime"`
	Encoding   string    `json:"encoding"`
	BOM        bool      `json:"bom"`
	EOL        string    `json:"eol"` // lf | crlf
	Editable   bool      `json:"editable"`
	Text       string    `json:"text"` // always \n line endings
	Offset     int64     `json:"offset"`
	NextOffset int64     `json:"next_offset"` // -1 at EOF
}

func (s *Server) handleFilesText(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetText(w, r)
	case http.MethodPut:
		s.handlePutText(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// looksBinary: a NUL byte, or a signature DetectContentType recognizes as
// non-text. Its generic "application/octet-stream" verdict is NOT binary:
// it fires on bytes 0x0E–0x1F, which CP437 NFO art uses.
func looksBinary(sample []byte) bool {
	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}
	ct := http.DetectContentType(sample)
	return !strings.HasPrefix(ct, "text/") && ct != "application/octet-stream"
}

func defaultLegacyEncoding(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".nfo", ".diz":
		return "cp437"
	}
	return "windows-1252"
}

// trimRunes drops a partial UTF-8 sequence at the start (unless atStart)
// and an incomplete trailing sequence (unless atEOF). head is the number of
// bytes cut from the front.
func trimRunes(b []byte, atStart, atEOF bool) (out []byte, head int) {
	if !atStart {
		for head < len(b) && head < utf8.UTFMax && !utf8.RuneStart(b[head]) {
			head++
		}
	}
	b = b[head:]
	if !atEOF {
		for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
			if utf8.RuneStart(b[i]) {
				if !utf8.FullRune(b[i:]) {
					b = b[:i]
				}
				break
			}
		}
	}
	return b, head
}

// detectEOL returns "crlf" when CRLF line endings are at least as common
// as bare LF ones.
func detectEOL(b []byte) string {
	crlf := bytes.Count(b, []byte("\r\n"))
	if crlf > 0 && 2*crlf >= bytes.Count(b, []byte("\n")) {
		return "crlf"
	}
	return "lf"
}

// GET /api/files/text?path=ABS[&encoding=utf-8|cp437|windows-1252][&offset=N]
func (s *Server) handleGetText(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	enc := q.Get("encoding")
	if _, ok := legacyEncodings[enc]; enc != "" && enc != "utf-8" && !ok {
		writeError(w, http.StatusBadRequest, "unknown encoding "+enc)
		return
	}
	p := filepath.Clean(q.Get("path"))
	real, info, status, msg := s.resolveRootFile(p)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	f, err := os.Open(real)
	if err != nil {
		writeError(w, statusFor(fscheck.Describe(err, filepath.Dir(p))), "cannot open file")
		return
	}
	defer f.Close()

	sample := make([]byte, sniffLen)
	n, _ := io.ReadFull(f, sample)
	if looksBinary(sample[:n]) {
		writeError(w, http.StatusUnsupportedMediaType, "not a text file")
		return
	}

	size := info.Size()
	resp := textFileResponse{Name: filepath.Base(p), Size: size, Mtime: info.ModTime(), NextOffset: -1}
	var off, end int64 = 0, size
	if size <= textEditMax {
		resp.Editable = true
	} else {
		if o := q.Get("offset"); o != "" {
			if off, err = strconv.ParseInt(o, 10, 64); err != nil || off < -1 {
				writeError(w, http.StatusBadRequest, "invalid offset")
				return
			}
		}
		if off == -1 {
			off = size - textWindow
		}
		off = min(off, size)
		end = min(off+textWindow, size)
	}
	data := make([]byte, end-off)
	if _, err := f.ReadAt(data, off); err != nil && err != io.EOF {
		writeError(w, statusFor(fscheck.Describe(err, filepath.Dir(p))), "cannot read file")
		return
	}

	atStart, atEOF := off == 0, end == size
	if enc == "" || enc == "utf-8" {
		trimmed, head := trimRunes(data, atStart, atEOF)
		if utf8.Valid(trimmed) {
			enc, data, off = "utf-8", trimmed, off+int64(head)
		} else if enc == "utf-8" {
			writeError(w, http.StatusUnprocessableEntity, "file is not valid UTF-8 — choose CP437 or Windows-1252")
			return
		}
	}
	if enc == "" {
		enc = defaultLegacyEncoding(p)
	}
	resp.Offset = off
	if next := off + int64(len(data)); next < size {
		resp.NextOffset = next
	}
	resp.EOL = detectEOL(data)

	var text string
	if enc == "utf-8" {
		if atStart && bytes.HasPrefix(data, utf8BOM) {
			resp.BOM, data = true, data[len(utf8BOM):]
		}
		text = string(data)
	} else {
		dec, err := legacyEncodings[enc].cm.NewDecoder().Bytes(data)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "cannot decode as "+legacyEncodings[enc].label)
			return
		}
		text = string(dec)
	}
	resp.Encoding = enc
	resp.Text = strings.ReplaceAll(text, "\r\n", "\n")
	writeJSON(w, http.StatusOK, resp)
}

type textSaveRequest struct {
	Path          string    `json:"path"`
	Text          string    `json:"text"`
	Encoding      string    `json:"encoding"`
	BOM           bool      `json:"bom"`
	EOL           string    `json:"eol"`
	ExpectedMtime time.Time `json:"expected_mtime"`
	ExpectedSize  int64     `json:"expected_size"`
	Force         bool      `json:"force"`
}

// encodeText converts \n-terminated text to enc. A rune the target can't
// represent is an error naming its 1-based line:column (column in runes).
func encodeText(text, enc string) ([]byte, error) {
	if enc == "utf-8" {
		return []byte(text), nil // a JSON-decoded string is always valid UTF-8
	}
	le := legacyEncodings[enc]
	out := make([]byte, 0, len(text))
	line, col := 1, 0
	for _, r := range text {
		col++
		b, ok := le.cm.EncodeRune(r)
		if !ok {
			return nil, fmt.Errorf("character %q at line %d:%d not representable in %s", r, line, col, le.label)
		}
		out = append(out, b)
		if r == '\n' {
			line, col = line+1, 0
		}
	}
	return out, nil
}

// writeFileAtomic replaces path via a temp file in the same directory, so a
// crash leaves either the old or the new content, never a mix.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".xirc-edit-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// PUT /api/files/text (admin-only via adminRules).
func (s *Server) handlePutText(w http.ResponseWriter, r *http.Request) {
	var req textSaveRequest
	// Generous body cap: 2 MB of CP437 box art is ~6 MB of UTF-8 plus JSON escaping.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*textEditMax)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, ok := legacyEncodings[req.Encoding]; req.Encoding != "utf-8" && !ok {
		writeError(w, http.StatusBadRequest, "unknown encoding "+req.Encoding)
		return
	}
	if req.EOL != "lf" && req.EOL != "crlf" {
		writeError(w, http.StatusBadRequest, "eol must be lf or crlf")
		return
	}
	real, info, status, msg := s.resolveRootFile(req.Path)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	if info.Size() > textEditMax {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large to edit (max 2 MB)")
		return
	}
	if !req.Force && (!info.ModTime().Equal(req.ExpectedMtime) || info.Size() != req.ExpectedSize) {
		writeError(w, http.StatusConflict, "file changed on disk")
		return
	}

	data, err := encodeText(strings.ReplaceAll(req.Text, "\r\n", "\n"), req.Encoding)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if req.EOL == "crlf" { // 0x0A/0x0D are the same in all supported encodings
		data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	}
	if req.BOM && req.Encoding == "utf-8" {
		data = append(append([]byte{}, utf8BOM...), data...)
	}
	if len(data) > textEditMax {
		writeError(w, http.StatusRequestEntityTooLarge, "result too large to save (max 2 MB)")
		return
	}

	dir := filepath.Dir(real)
	if err := writeFileAtomic(real, data, info.Mode().Perm()); err != nil {
		err = fscheck.Describe(err, dir)
		writeError(w, statusFor(err), err.Error())
		return
	}
	ni, err := os.Stat(real)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saved, but cannot stat file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mtime": ni.ModTime(), "size": ni.Size()})
}
