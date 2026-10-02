package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

// Config export/import (Settings → Backup). Secrets — server auth_password,
// realm key — and settings.auth are never part of the file: the types below
// have no field for them, so they can't leak by accident.

const (
	configFormat   = "xirc-config"
	configVersion  = 1
	configMaxBytes = 1 << 20
)

type exportRealm struct {
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	SearchCommand   string `json:"search_command"`
	DownloadChannel string `json:"download_channel"`
	SearchBot       string `json:"search_bot"`
	SearchTimeout   int    `json:"search_timeout"`
	AutoJoin        bool   `json:"auto_join"`
	Enabled         bool   `json:"enabled"`
}

type exportServer struct {
	Name         string        `json:"name"`
	Host         string        `json:"host"`
	Port         int           `json:"port"`
	SSL          bool          `json:"ssl"`
	Nickname     string        `json:"nickname"`
	AltNicknames []string      `json:"alt_nicknames"`
	AuthMethod   string        `json:"auth_method"`
	AutoConnect  bool          `json:"auto_connect"`
	Enabled      bool          `json:"enabled"`
	Realms       []exportRealm `json:"realms"`
}

// exportSettings is config.Editable minus Auth.
type exportSettings struct {
	Storage     config.EditableStorage   `json:"storage"`
	Downloads   config.DownloadsConfig   `json:"downloads"`
	Maintenance config.MaintenanceConfig `json:"maintenance"`
	UI          config.UIConfig          `json:"ui"`
}

type configFile struct {
	Format     string          `json:"format"`
	Version    int             `json:"version"`
	ExportedAt time.Time       `json:"exported_at"`
	Servers    []exportServer  `json:"servers"`
	Settings   *exportSettings `json:"settings,omitempty"`
}

func toExportServer(s db.Server) exportServer {
	alt := s.AltNicknames
	if alt == nil {
		alt = []string{}
	}
	return exportServer{Name: s.Name, Host: s.Host, Port: s.Port, SSL: s.SSL, Nickname: s.Nickname,
		AltNicknames: alt, AuthMethod: s.AuthMethod, AutoConnect: s.AutoConnect, Enabled: s.Enabled,
		Realms: []exportRealm{}}
}

// applyTo copies the exported fields onto dst; ID, AuthPassword and
// timestamps are left as they are.
func (e exportServer) applyTo(dst *db.Server) {
	dst.Name, dst.Host, dst.Port, dst.SSL, dst.Nickname = e.Name, e.Host, e.Port, e.SSL, e.Nickname
	dst.AltNicknames, dst.AuthMethod, dst.AutoConnect, dst.Enabled = e.AltNicknames, e.AuthMethod, e.AutoConnect, e.Enabled
}

func toExportRealm(r db.Realm) exportRealm {
	return exportRealm{Name: r.Name, DisplayName: r.DisplayName, SearchCommand: r.SearchCommand,
		DownloadChannel: r.DownloadChannel, SearchBot: r.SearchBot, SearchTimeout: r.SearchTimeout,
		AutoJoin: r.AutoJoin, Enabled: r.Enabled}
}

// applyTo copies the exported fields onto dst; ID, ServerID and Key are
// left as they are.
func (e exportRealm) applyTo(dst *db.Realm) {
	dst.Name, dst.DisplayName, dst.SearchCommand, dst.DownloadChannel = e.Name, e.DisplayName, e.SearchCommand, e.DownloadChannel
	dst.SearchBot, dst.SearchTimeout, dst.AutoJoin, dst.Enabled = e.SearchBot, e.SearchTimeout, e.AutoJoin, e.Enabled
}

func settingsFromEditable(e config.Editable) *exportSettings {
	return &exportSettings{Storage: e.Storage, Downloads: e.Downloads, Maintenance: e.Maintenance, UI: e.UI}
}

func parseIDList(s string) (map[int64]bool, error) {
	ids := map[int64]bool{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q", part)
		}
		ids[id] = true
	}
	return ids, nil
}

// GET /api/config/export?servers=1,2&realms=7&settings=1 — no params exports
// everything. A selected realm brings its parent server along (holding only
// the selected realms); a selected server brings all its realms.
func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	all := !q.Has("servers") && !q.Has("realms") && !q.Has("settings")
	srvIDs, err := parseIDList(q.Get("servers"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	realmIDs, err := parseIDList(q.Get("realms"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	servers, err := s.store.GetServers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	f := configFile{Format: configFormat, Version: configVersion, ExportedAt: time.Now().UTC(), Servers: []exportServer{}}
	for _, sv := range servers {
		realms, err := s.store.GetRealms(sv.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		full := all || srvIDs[sv.ID]
		es := toExportServer(sv)
		for _, rl := range realms {
			if full || realmIDs[rl.ID] {
				es.Realms = append(es.Realms, toExportRealm(rl))
			}
		}
		if full || len(es.Realms) > 0 {
			f.Servers = append(f.Servers, es)
		}
	}
	if (all || q.Get("settings") == "1") && s.settings.cfg != nil {
		s.settings.mu.Lock()
		f.Settings = settingsFromEditable(s.settings.cfg.Editable())
		s.settings.mu.Unlock()
	}
	w.Header().Set("Content-Disposition", `attachment; filename="xirc-config-`+time.Now().Format("2006-01-02")+`.json"`)
	writeJSON(w, http.StatusOK, f)
}
