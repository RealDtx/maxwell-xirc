package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
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

	rawSettings json.RawMessage // as in the file, so missing fields keep current values
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

// decodeConfigFile rejects anything that isn't a config export this build
// understands. Unknown top-level keys are ignored (forward compatibility).
func decodeConfigFile(raw []byte) (*configFile, error) {
	var f configFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("not a valid JSON file: %v", err)
	}
	if f.Format != configFormat {
		return nil, fmt.Errorf("not an xirc config export")
	}
	if f.Version != configVersion {
		return nil, fmt.Errorf("unsupported config version %d (this build reads %d)", f.Version, configVersion)
	}
	var rs struct {
		Settings json.RawMessage `json:"settings"`
	}
	_ = json.Unmarshal(raw, &rs) // already parsed once above
	f.rawSettings = rs.Settings
	return &f, nil
}

// readLimited reads at most max bytes of the request body.
func readLimited(w http.ResponseWriter, r *http.Request, max int64) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return nil, fmt.Errorf("file too large (max %d MB)", max>>20)
	}
	return raw, err
}

type importItem struct {
	Kind    string   `json:"kind"`   // server | realm | settings
	Key     string   `json:"key"`    // "srv", "srv/#chan", "settings"
	Status  string   `json:"status"` // new | exists | error
	Changes []string `json:"changes"`
	Error   string   `json:"error"`
}

type plannedItem struct {
	importItem
	srv      *exportServer // kind server
	realm    *exportRealm  // kind realm
	parent   string        // kind realm: parent server name
	existing *db.Server    // kind server: matched row
	exRealm  *db.Realm     // kind realm: matched row
}

func validateExportServer(e exportServer) string {
	switch {
	case strings.TrimSpace(e.Host) == "":
		return "host is required"
	case e.Port < 1 || e.Port > 65535:
		return "port must be 1–65535"
	case strings.TrimSpace(e.Nickname) == "":
		return "nickname is required"
	}
	return ""
}

// settingsCandidate is cur with the file's sections swapped in; auth and
// env-locked keys keep their current values.
func settingsCandidate(cur config.Editable, raw json.RawMessage) config.Editable {
	e := cur
	fs := settingsFromEditable(cur)
	_ = json.Unmarshal(raw, fs) // fields missing from the file keep their current values
	e.Storage, e.Downloads, e.Maintenance, e.UI = fs.Storage, fs.Downloads, fs.Maintenance, fs.UI
	keepLocked(cur, &e, config.EnvLockedKeys())
	return e
}

// planImport matches every item in f against the store and validates it.
// It never writes. Items come back in file order (each server followed by
// its realms), settings last.
func (s *Server) planImport(f *configFile) ([]plannedItem, error) {
	servers, err := s.store.GetServers()
	if err != nil {
		return nil, err
	}
	byName := map[string]*db.Server{}
	nameCount := map[string]int{}
	for i := range servers {
		byName[servers[i].Name] = &servers[i]
		nameCount[servers[i].Name]++
	}

	var items []plannedItem
	seen := map[string]bool{}
	for i := range f.Servers {
		es := &f.Servers[i]
		it := plannedItem{importItem: importItem{Kind: "server", Key: es.Name, Changes: []string{}}, srv: es}
		switch {
		case strings.TrimSpace(es.Name) == "":
			it.Error = "server name is required"
		case seen[es.Name]:
			it.Error = "duplicate server " + es.Name + " in file"
		default:
			it.Error = validateExportServer(*es)
		}
		if n := nameCount[es.Name]; it.Error == "" && n > 1 {
			it.Error = fmt.Sprintf("%d servers named %s on this instance — rename one first", n, es.Name)
		}
		seen[es.Name] = true

		var exRealms []db.Realm
		if ex := byName[es.Name]; ex != nil && it.Error == "" {
			it.existing = ex
			if exRealms, err = s.store.GetRealms(ex.ID); err != nil {
				return nil, err
			}
			a, b := toExportServer(*ex), *es
			a.Realms, b.Realms = nil, nil
			it.Changes = diffFields(a, b)
		}
		it.Status = itemStatus(it.Error, it.existing != nil)
		items = append(items, it)

		for j := range es.Realms {
			er := &es.Realms[j]
			ri := plannedItem{importItem: importItem{Kind: "realm", Key: es.Name + "/" + er.Name, Changes: []string{}},
				realm: er, parent: es.Name}
			switch {
			case it.Error != "":
				ri.Error = "parent server has errors"
			case strings.TrimSpace(er.Name) == "":
				ri.Error = "realm name is required"
			case seen[ri.Key]:
				ri.Error = "duplicate realm " + ri.Key + " in file"
			}
			seen[ri.Key] = true
			if ri.Error == "" {
				n := 0
				for k := range exRealms {
					if exRealms[k].Name == er.Name {
						n++
						ri.exRealm = &exRealms[k]
					}
				}
				if n > 1 {
					ri.exRealm = nil
					ri.Error = fmt.Sprintf("%d realms named %s on server %s — rename one first", n, er.Name, es.Name)
				} else if n == 1 {
					ri.Changes = diffFields(toExportRealm(*ri.exRealm), *er)
				}
			}
			ri.Status = itemStatus(ri.Error, ri.exRealm != nil)
			items = append(items, ri)
		}
	}

	if f.Settings != nil {
		it := plannedItem{importItem: importItem{Kind: "settings", Key: "settings", Changes: []string{}}}
		if s.settings.cfg == nil {
			it.Error = "settings are not available on this server"
		} else {
			s.settings.mu.Lock()
			cur := s.settings.cfg.Editable()
			s.settings.mu.Unlock()
			e := settingsCandidate(cur, f.rawSettings)
			if errs := validateSettings(cur, e); len(errs) > 0 {
				it.Error = strings.Join(errs, "; ")
			}
			it.Changes = diffFields(settingsFromEditable(cur), settingsFromEditable(e))
		}
		it.Status = itemStatus(it.Error, true)
		items = append(items, it)
	}
	return items, nil
}

func itemStatus(errMsg string, exists bool) string {
	switch {
	case errMsg != "":
		return "error"
	case exists:
		return "exists"
	}
	return "new"
}

// diffFields lists `field: old→new` for every JSON field that differs
// between a and b (values of the same type); nested objects flatten to
// "section.field". Sorted for stable output.
func diffFields(a, b any) []string {
	var am, bm map[string]any
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	json.Unmarshal(ja, &am)
	json.Unmarshal(jb, &bm)
	out := []string{}
	diffMaps("", am, bm, &out)
	sort.Strings(out)
	return out
}

func diffMaps(prefix string, a, b map[string]any, out *[]string) {
	for k, bv := range b {
		av := a[k]
		am, aok := av.(map[string]any)
		bmm, bok := bv.(map[string]any)
		if aok && bok {
			diffMaps(prefix+k+".", am, bmm, out)
			continue
		}
		if !reflect.DeepEqual(av, bv) {
			ja, _ := json.Marshal(av)
			jb, _ := json.Marshal(bv)
			*out = append(*out, prefix+k+": "+string(ja)+"→"+string(jb))
		}
	}
}

// POST /api/config/import/preview — body is the export file.
func (s *Server) handleConfigImportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw, err := readLimited(w, r, configMaxBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := decodeConfigFile(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := s.planImport(f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]importItem, len(items))
	for i := range items {
		out[i] = items[i].importItem
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type importResult struct {
	Kind   string `json:"kind"`
	Key    string `json:"key"`
	Result string `json:"result"` // created | updated | skipped | excluded | error
	Error  string `json:"error"`
}

type importApplyRequest struct {
	File      json.RawMessage   `json:"file"`
	Default   string            `json:"default"`   // skip | overwrite
	Decisions map[string]string `json:"decisions"` // key → skip | overwrite | exclude
}

// POST /api/config/import/apply — stateless: the file is sent again and
// re-planned, since the DB may have changed since the preview.
func (s *Server) handleConfigImportApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw, err := readLimited(w, r, 2*configMaxBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req importApplyRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Default != "skip" && req.Default != "overwrite" {
		writeError(w, http.StatusBadRequest, "default must be skip or overwrite")
		return
	}
	for k, d := range req.Decisions {
		if d != "skip" && d != "overwrite" && d != "exclude" {
			writeError(w, http.StatusBadRequest, "decision for "+k+" must be skip, overwrite or exclude")
			return
		}
	}
	if len(req.File) > configMaxBytes {
		writeError(w, http.StatusBadRequest, "file too large (max 1 MB)")
		return
	}
	f, err := decodeConfigFile(req.File)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	results, err := s.applyImport(f, req.Default, req.Decisions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

// applyImport writes servers, then realms, then settings — best effort per
// item (the store has no cross-driver transaction); results keep file order.
func (s *Server) applyImport(f *configFile, def string, decisions map[string]string) ([]importResult, error) {
	items, err := s.planImport(f)
	if err != nil {
		return nil, err
	}
	results := make([]importResult, len(items))
	for i := range items {
		results[i] = importResult{Kind: items[i].Kind, Key: items[i].Key}
	}
	// action maps an item to error | excluded | skipped | create | update.
	action := func(it *plannedItem) string {
		if it.Status == "error" {
			return "error"
		}
		d := decisions[it.Key]
		switch {
		case d == "exclude":
			return "excluded"
		case it.Status == "new":
			if d == "skip" {
				return "skipped" // explicit only; the global default never blocks creation
			}
			return "create"
		case d == "":
			d = def
		}
		if d == "skip" {
			return "skipped"
		}
		return "update"
	}
	fail := func(res *importResult, msg string) { res.Result, res.Error = "error", msg }

	// Pass 1: servers. serverIDs maps a server name to the row its realms go into.
	serverIDs := map[string]int64{}
	for i := range items {
		it, res := &items[i], &results[i]
		if it.Kind != "server" {
			continue
		}
		if it.existing != nil {
			serverIDs[it.Key] = it.existing.ID
		}
		switch a := action(it); a {
		case "error":
			fail(res, it.Error)
		case "excluded", "skipped":
			res.Result = a
		case "create":
			var srv db.Server
			it.srv.applyTo(&srv)
			if err := s.store.CreateServer(&srv); err != nil {
				fail(res, err.Error())
				continue
			}
			serverIDs[it.Key] = srv.ID
			res.Result = "created"
			if s.ircMgr != nil {
				s.ircMgr.ReloadServer(srv.ID)
			}
		case "update":
			srv := *it.existing // keeps ID and AuthPassword
			it.srv.applyTo(&srv)
			if err := s.store.UpdateServer(&srv); err != nil {
				fail(res, err.Error())
				continue
			}
			res.Result = "updated"
			if len(it.Changes) > 0 && s.ircMgr != nil {
				s.ircMgr.ReloadServer(srv.ID)
			}
		}
	}

	// Pass 2: realms.
	touched := map[int64]bool{}
	for i := range items {
		it, res := &items[i], &results[i]
		if it.Kind != "realm" {
			continue
		}
		a := action(it)
		switch a {
		case "error":
			fail(res, it.Error)
			continue
		case "excluded", "skipped":
			res.Result = a
			continue
		}
		sid, ok := serverIDs[it.parent]
		if !ok {
			fail(res, "parent server not imported")
			continue
		}
		var rl db.Realm
		if a == "update" {
			rl = *it.exRealm // keeps ID and Key
		}
		it.realm.applyTo(&rl)
		rl.ServerID = sid
		if a == "create" {
			err = s.store.CreateRealm(&rl)
			res.Result = "created"
		} else {
			err = s.store.UpdateRealm(&rl)
			res.Result = "updated"
		}
		if err != nil {
			fail(res, err.Error())
			continue
		}
		if a == "create" || len(it.Changes) > 0 {
			touched[sid] = true
		}
	}
	if s.ircMgr != nil {
		for sid := range touched {
			s.ircMgr.ReloadRealms(sid)
		}
	}

	// Pass 3: settings, through the same path as PUT /api/settings.
	for i := range items {
		it, res := &items[i], &results[i]
		if it.Kind != "settings" {
			continue
		}
		switch a := action(it); a {
		case "error":
			fail(res, it.Error)
		case "excluded", "skipped":
			res.Result = a
		default:
			s.settings.mu.Lock()
			_, errs, err := s.applySettings(settingsCandidate(s.settings.cfg.Editable(), f.rawSettings))
			s.settings.mu.Unlock()
			switch {
			case len(errs) > 0:
				fail(res, strings.Join(errs, "; "))
			case err != nil:
				fail(res, "failed to save settings: "+err.Error())
			default:
				res.Result = "updated"
			}
		}
	}
	return results, nil
}
