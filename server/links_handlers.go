package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/dcc"
	"github.com/RealDtx/maxwell-irc/links"
)

const maxLinksBody = 1 << 20

// linkRow is one parsed link as shown in the paste preview.
type linkRow struct {
	Line       string     `json:"line"`
	ServerID   int64      `json:"server_id"`
	ServerName string     `json:"server_name"`
	Channel    string     `json:"channel"`
	Bot        string     `json:"bot"`
	Pack       int        `json:"pack"`
	Name       string     `json:"name"`
	Size       string     `json:"size"`
	First      *time.Time `json:"first,omitempty"`
	Last       *time.Time `json:"last,omitempty"`
	Status     string     `json:"status"`
}

// readLinksBody accepts raw text, or JSON {"text": "..."} / {"links": [...]}.
func readLinksBody(w http.ResponseWriter, r *http.Request) (string, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLinksBody))
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			Text  string   `json:"text"`
			Links []string `json:"links"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return "", err
		}
		return body.Text + "\n" + strings.Join(body.Links, "\n"), nil
	}
	return string(raw), nil
}

// resolveLinks parses text and classifies every link against the configured
// servers and existing downloads.
func (s *Server) resolveLinks(text string) ([]linkRow, []links.LineError, error) {
	parsed, bad := links.Parse(text)
	servers, err := s.store.GetServers()
	if err != nil {
		return nil, nil, err
	}
	dls, err := s.store.GetDownloads("")
	if err != nil {
		return nil, nil, err
	}
	rows := make([]linkRow, 0, len(parsed))
	for _, l := range parsed {
		row := linkRow{Line: links.Format(l), Channel: l.Channel, Bot: l.Bot, Pack: l.Pack, Name: l.Name, Size: l.Size, Status: "ok"}
		if !l.First.IsZero() {
			row.First = &l.First
		}
		if !l.Last.IsZero() {
			row.Last = &l.Last
		}
		srv, ok := links.ResolveServer(l.Host, servers)
		if !ok {
			row.Status = "unknown network"
		} else {
			row.ServerID, row.ServerName = srv.ID, srv.Name
			row.Status = duplicateStatus(dls, srv.ID, l)
		}
		rows = append(rows, row)
	}
	return rows, bad, nil
}

// duplicateStatus flags links already in the download list: an active
// download of the same bot+pack or file, or a completed one of the same file.
func duplicateStatus(dls []db.Download, serverID int64, l links.Link) string {
	for _, d := range dls {
		if d.ServerID != serverID {
			continue
		}
		sameFile := l.Name != "" && strings.EqualFold(d.Filename, l.Name)
		samePack := strings.EqualFold(d.BotNick, l.Bot) && d.PackNumber == l.Pack
		switch d.Status {
		case "queued", "downloading", "processing":
			if sameFile || samePack {
				return "already queued"
			}
		case "completed":
			if sameFile {
				return "already downloaded"
			}
		}
	}
	return "ok"
}

func (s *Server) handleLinksPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	text, err := readLinksBody(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	rows, bad, err := s.resolveLinks(text)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if bad == nil {
		bad = []links.LineError{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"links": rows, "errors": bad})
}

func (s *Server) handleLinksQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if c := s.capabilities().Downloads; !c.OK {
		writeError(w, http.StatusServiceUnavailable, "downloads disabled: "+c.Reason)
		return
	}
	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}
	text, err := readLinksBody(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	rows, bad, err := s.resolveLinks(text)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	skipped := append([]links.LineError{}, bad...)
	queued := 0
	for _, row := range rows {
		if row.Status != "ok" {
			skipped = append(skipped, links.LineError{Line: row.Line, Reason: row.Status})
			continue
		}
		size, _ := dcc.ParseSize(row.Size) // optional; unknown size stays 0
		if _, err := s.queueDownload(downloadRequest{ServerID: row.ServerID, Channel: row.Channel, BotNick: row.Bot,
			PackNumber: row.Pack, Filename: row.Name, Filesize: size}); err != nil {
			skipped = append(skipped, links.LineError{Line: row.Line, Reason: err.Error()})
			continue
		}
		queued++
	}
	if queued > 0 {
		s.engine.TryDispatchQueued()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"queued": queued, "skipped": skipped})
}
