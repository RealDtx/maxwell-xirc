package server

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/dcc"
	"github.com/RealDtx/maxwell-irc/fscheck"
	"github.com/RealDtx/maxwell-irc/maintenance"
	"gopkg.in/yaml.v3"
)

// settingsState carries what the /api/settings handler needs to validate,
// persist and live-apply an Editable — wired once at startup via SetSettings.
type settingsState struct {
	mu    sync.Mutex // held for the whole PUT, so concurrent saves serialize
	path  string
	cfg   *config.Config
	maint *maintenance.Maintenance
}

// SetSettings wires the admin settings API in — set once at startup,
// alongside SetAuth/SetLibrary.
func (s *Server) SetSettings(path string, cfg *config.Config, maint *maintenance.Maintenance) {
	s.settings.path = path
	s.settings.cfg = cfg
	s.settings.maint = maint
}

type settingsReadonlyServer struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Prefix string `json:"prefix"`
}

// settingsReadonlyDatabase deliberately omits the DSN field: it may carry a
// password and is never returned by any API.
type settingsReadonlyDatabase struct {
	Driver string `json:"driver"`
	Path   string `json:"path"`
}

type settingsReadonly struct {
	Server   settingsReadonlyServer   `json:"server"`
	Database settingsReadonlyDatabase `json:"database"`
	// MediaDir is storage.media_dir from config.yaml. The Library settings
	// page is the live source of truth for the media root; this is informational.
	MediaDir   string `json:"media_dir"`
	ConfigPath string `json:"config_path"`
}

type persistStatus struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

type settingsResponse struct {
	Settings config.Editable  `json:"settings"`
	Locked   []string         `json:"locked"`
	Persist  persistStatus    `json:"persist"`
	Readonly settingsReadonly `json:"readonly"`
	Warnings []string         `json:"warnings"`
}

// buildSettingsResponse must be called with s.settings.mu held, since it
// reads cfg fields that ApplyEditable mutates under the same lock.
func (s *Server) buildSettingsResponse(cfg *config.Config, warnings []string) settingsResponse {
	locked := config.EnvLockedKeys()
	if locked == nil {
		locked = []string{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	pc := writableOrCreatable(filepath.Dir(s.settings.path))
	return settingsResponse{
		Settings: cfg.Editable(),
		Locked:   locked,
		Persist:  persistStatus{OK: pc.OK, Reason: pc.Reason},
		Readonly: settingsReadonly{
			Server:     settingsReadonlyServer{Host: cfg.Server.Host, Port: cfg.Server.Port, Prefix: cfg.Server.Prefix},
			Database:   settingsReadonlyDatabase{Driver: cfg.Database.Driver, Path: cfg.Database.Path},
			MediaDir:   cfg.Storage.MediaDir,
			ConfigPath: s.settings.path,
		},
		Warnings: warnings,
	}
}

// GET/PUT /api/settings — admin-only (see adminRules). GET returns the
// current editable settings plus read-only context; PUT validates,
// persists to config.yaml, applies live, and returns the same shape.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings.cfg == nil {
		writeError(w, http.StatusInternalServerError, "settings not initialized")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.settings.mu.Lock()
		resp := s.buildSettingsResponse(s.settings.cfg, nil)
		s.settings.mu.Unlock()
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPut:
		s.handlePutSettings(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()

	// Decode onto the current values so omitted sections/fields keep them.
	e := s.settings.cfg.Editable()
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	warnings, errs, err := s.applySettings(e)
	if len(errs) > 0 {
		writeError(w, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.buildSettingsResponse(s.settings.cfg, warnings))
}

// applySettings validates e against the current settings, persists it to
// config.yaml and applies it live. Caller must hold s.settings.mu. errs are
// validation failures, err a persist failure; in both cases nothing is applied.
// Shared by PUT /api/settings and config import.
func (s *Server) applySettings(e config.Editable) (warnings, errs []string, err error) {
	cfg := s.settings.cfg
	cur := cfg.Editable()
	if errs := validateSettings(cur, e); len(errs) > 0 {
		return nil, errs, nil
	}
	locked := config.EnvLockedKeys()
	if errs := checkEnvLocks(cur, e, locked); len(errs) > 0 {
		return nil, errs, nil
	}

	// Env-locked fields are already equal to the current (env-derived) value
	// by this point — but the FILE's own value for that key may differ (the
	// env var only overrides it in memory). Omit locked keys from what's
	// saved so the merge in SaveKeys leaves the file's value untouched.
	toSave, err := settingsToSave(e, locked)
	if err != nil {
		return nil, nil, err
	}
	if err := config.SaveKeys(s.settings.path, toSave); err != nil {
		if fscheck.IsPermission(err) {
			return nil, nil, fscheck.Describe(err, filepath.Dir(s.settings.path))
		}
		return nil, nil, err
	}

	if s.engine != nil {
		s.engine.SetRuntime(e.Storage.DownloadsDir, e.Storage.TempDir, e.Storage.MinFreeSpace, e.Downloads.MaxConcurrent)
	}
	s.setStorageDirs(e.Storage.DownloadsDir, e.Storage.TempDir)
	if s.auth != nil {
		if err := s.auth.Update(e.Auth); err != nil {
			// e.Auth was already validated by ParseAuthPolicy in validateSettings,
			// so Update should never fail here — log rather than silently drop it.
			log.Printf("settings: auth.Update failed after passing validation: %v", err)
		}
	}
	if s.settings.maint != nil {
		s.settings.maint.SetConfig(e.Maintenance)
	}
	cfg.ApplyEditable(e)

	go s.RecheckCapabilities()

	if e.Auth.TrustedRole == "admin" {
		for _, n := range e.Auth.TrustedNetworks {
			if isOpenNetwork(n) {
				warnings = append(warnings, "every client that reaches xirc gets admin")
				break
			}
		}
	}
	return warnings, nil, nil
}

// isOpenNetwork reports whether cidr matches every address (a /0 prefix —
// "0.0.0.0/0", "::/0", "0::/0", …), detected by parsing rather than string
// matching so any equivalent spelling is caught.
func isOpenNetwork(cidr string) bool {
	if !strings.Contains(cidr, "/") {
		return false // a bare IP is never "everything"
	}
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	ones, _ := n.Mask.Size()
	return ones == 0
}

// settingsToSave returns v as-is when nothing is locked; otherwise it
// round-trips through YAML to a nested map and deletes each locked dotted
// key, so SaveKeys' merge skips that key and leaves the file's own value.
func settingsToSave(e config.Editable, locked []string) (any, error) {
	if len(locked) == 0 {
		return e, nil
	}
	data, err := yaml.Marshal(e)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for _, dotted := range locked {
		parts := strings.SplitN(dotted, ".", 2)
		if len(parts) != 2 {
			continue
		}
		if sec, ok := m[parts[0]].(map[string]any); ok {
			delete(sec, parts[1])
		}
	}
	return m, nil
}

// validateSettings checks e; writability is only checked for dirs that
// differ from cur, so a currently-broken dir (unmounted disk, env-locked
// Docker dir) doesn't block saving unrelated settings.
func validateSettings(cur, e config.Editable) []string {
	var errs []string
	for _, d := range []struct{ key, dir, old string }{
		{"storage.downloads_dir", e.Storage.DownloadsDir, cur.Storage.DownloadsDir},
		{"storage.temp_dir", e.Storage.TempDir, cur.Storage.TempDir},
	} {
		if !filepath.IsAbs(d.dir) {
			errs = append(errs, d.key+" must be an absolute path")
		} else if d.dir == d.old {
			continue
		} else if c := writableOrCreatable(d.dir); !c.OK {
			errs = append(errs, d.key+": "+c.Reason)
		}
	}
	if _, err := dcc.ParseSize(e.Storage.MinFreeSpace); err != nil {
		errs = append(errs, "storage.min_free_space: "+err.Error())
	}
	if n := e.Downloads.MaxConcurrent; n < 1 || n > 20 {
		errs = append(errs, "downloads.max_concurrent must be 1–20")
	}
	m := e.Maintenance
	if m.SearchResultRetentionDays < 0 || m.IndexMaxFiles < 0 || m.IntervalHours < 0 {
		errs = append(errs, "maintenance values must be 0 or more")
	}
	if err := ParseAuthPolicy(e.Auth); err != nil {
		errs = append(errs, err.Error())
	}
	switch e.UI.HelpDefault {
	case "first_time", "always", "never":
	default:
		errs = append(errs, "ui.help_default must be first_time, always or never")
	}
	return errs
}

// checkEnvLocks compares current vs submitted for every dotted key in
// locked (as produced by config.EnvLockedKeys() — the single source of
// truth for which fields have an env override set). nil and empty slices
// compare equal, so an unset list field submitted as [] isn't flagged.
func checkEnvLocks(current, submitted config.Editable, locked []string) []string {
	var errs []string
	for _, dotted := range locked {
		parts := strings.SplitN(dotted, ".", 2)
		if len(parts) != 2 {
			continue
		}
		cf, ok1 := dottedField(reflect.ValueOf(current), parts[0], parts[1])
		sf, ok2 := dottedField(reflect.ValueOf(submitted), parts[0], parts[1])
		if !ok1 || !ok2 || valuesEqual(cf.Interface(), sf.Interface()) {
			continue
		}
		envKey := "XIRC_" + strings.ToUpper(parts[0]) + "_" + strings.ToUpper(parts[1])
		errs = append(errs, dotted+" is set by environment variable "+envKey+" and can't be changed here")
	}
	return errs
}

// dottedField returns the Editable field named by a section/key pair, as
// EnvLockedKeys' dotted keys split into (e.g. "downloads", "max_concurrent").
// v must be a config.Editable value (settable if the caller wants to Set).
func dottedField(v reflect.Value, section, key string) (reflect.Value, bool) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0] != section {
			continue
		}
		sv := v.Field(i)
		st := sv.Type()
		for j := 0; j < st.NumField(); j++ {
			if strings.Split(st.Field(j).Tag.Get("yaml"), ",")[0] == key {
				return sv.Field(j), true
			}
		}
	}
	return reflect.Value{}, false
}

// keepLocked resets every env-locked key in e to its current value, so a
// config import never fights an XIRC_* override (checkEnvLocks would reject it).
func keepLocked(cur config.Editable, e *config.Editable, locked []string) {
	for _, dotted := range locked {
		parts := strings.SplitN(dotted, ".", 2)
		if len(parts) != 2 {
			continue
		}
		cf, ok1 := dottedField(reflect.ValueOf(cur), parts[0], parts[1])
		ef, ok2 := dottedField(reflect.ValueOf(e).Elem(), parts[0], parts[1])
		if ok1 && ok2 {
			ef.Set(cf)
		}
	}
}

func valuesEqual(a, b any) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if av.Kind() == reflect.Slice && bv.Kind() == reflect.Slice && av.Len() == 0 && bv.Len() == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
