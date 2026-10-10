package server

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/links"
)

const (
	maxIndexImportBytes   = 128 << 20 // upload, as sent (gzip or text)
	maxIndexImportDecoded = 1 << 30   // after gunzip: guards against gzip bombs
	indexImportBatch      = 1000
)

func (s *Server) handleIndexExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ids, err := parseIDList(r.URL.Query().Get("servers"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	servers, err := s.store.GetServers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hosts := map[int64]string{}
	for _, sv := range servers {
		hosts[sv.ID] = sv.Host
	}
	var filter []int64
	for id := range ids {
		filter = append(filter, id)
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="xirc-index-%s.txt.gz"`, time.Now().Format("2006-01-02")))
	gz := gzip.NewWriter(w)
	bw := bufio.NewWriter(gz)
	err = s.store.ForEachIndexedFile(filter, func(f *db.IndexedFile) error {
		host, ok := hosts[f.ServerID]
		if !ok || f.PackNumber == nil {
			return nil // links need a network and a pack number
		}
		l := links.Link{Host: host, Channel: f.Channel, Bot: f.BotNick, Pack: *f.PackNumber,
			Name: f.Filename, First: f.FirstSeenAt, Last: f.LastSeenAt}
		if f.Filesize != nil {
			l.Size = *f.Filesize
		}
		_, err := bw.WriteString(links.Format(l) + "\n")
		return err
	})
	if err != nil {
		// Headers are sent; the truncated gzip stream makes the client's file invalid.
		log.Printf("index export: %v", err)
		return
	}
	if err := bw.Flush(); err != nil {
		log.Printf("index export: %v", err)
		return
	}
	gz.Close()
}

type indexImportResult struct {
	Added          int `json:"added"`
	Merged         int `json:"merged"`
	Stale          int `json:"stale"`
	UnknownNetwork int `json:"skipped_unknown_network"`
	Invalid        int `json:"invalid"`
}

func (s *Server) handleIndexImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	body := bufio.NewReader(http.MaxBytesReader(w, r.Body, maxIndexImportBytes))
	var src io.Reader = body
	if magic, _ := body.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid gzip file")
			return
		}
		defer gz.Close()
		src = io.LimitReader(gz, maxIndexImportDecoded)
	}

	servers, err := s.store.GetServers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byHost := map[string]*db.Server{} // resolved once per distinct host; nil = unknown
	var res indexImportResult
	batch := make([]db.IndexedFile, 0, indexImportBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		m, err := s.store.BulkMergeIndexedFiles(batch)
		res.Added += m.Added
		res.Merged += m.Merged
		res.Stale += m.Stale
		batch = batch[:0]
		return err
	}

	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		parsed, bad := links.Parse(sc.Text())
		res.Invalid += len(bad)
		for _, l := range parsed {
			srv, seen := byHost[l.Host]
			if !seen {
				srv, _ = links.ResolveServer(l.Host, servers)
				byHost[l.Host] = srv
			}
			if srv == nil {
				res.UnknownNetwork++
				continue
			}
			if l.Name == "" {
				res.Invalid++ // index entries need a filename
				continue
			}
			pack := l.Pack
			f := db.IndexedFile{ServerID: srv.ID, Channel: l.Channel, BotNick: l.Bot, PackNumber: &pack,
				Filename: l.Name, RawLine: links.Format(l), FirstSeenAt: l.First, LastSeenAt: l.Last}
			if l.Size != "" {
				size := l.Size
				f.Filesize = &size
			}
			batch = append(batch, f)
			if len(batch) == indexImportBatch {
				if err := flush(); err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		writeError(w, http.StatusBadRequest, "reading import: "+err.Error())
		return
	}
	if err := flush(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if s.settings.cfg != nil {
		s.settings.mu.Lock()
		max := s.settings.cfg.Maintenance.IndexMaxFiles
		s.settings.mu.Unlock()
		if _, err := s.store.EnforceIndexCap(int64(max)); err != nil {
			log.Printf("index import: enforcing cap: %v", err)
		}
	}
	writeJSON(w, http.StatusOK, res)
}
