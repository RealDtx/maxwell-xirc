package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/dcc"
	"github.com/RealDtx/maxwell-irc/fscheck"
	"github.com/RealDtx/maxwell-irc/maintenance"
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
	var e config.Editable
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if errs := validateSettings(e); len(errs) > 0 {
		writeError(w, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}

	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	cfg := s.settings.cfg

	if errs := checkEnvLocks(cfg.Editable(), e); len(errs) > 0 {
		writeError(w, http.StatusBadRequest, strings.Join(errs, "; "))
		return
	}

	if err := config.SaveKeys(s.settings.path, e); err != nil {
		reason := err.Error()
		status := http.StatusInternalServerError
		if fscheck.IsPermission(err) {
			reason = fscheck.Describe(err, filepath.Dir(s.settings.path)).Error()
			status = http.StatusForbidden
		}
		writeError(w, status, "failed to save settings: "+reason)
		return
	}

	if s.engine != nil {
		s.engine.SetRuntime(e.Storage.DownloadsDir, e.Storage.TempDir, e.Storage.MinFreeSpace, e.Downloads.MaxConcurrent)
	}
	s.setStorageDirs(e.Storage.DownloadsDir, e.Storage.TempDir)
	if s.auth != nil {
		s.auth.Update(e.Auth) // already validated by validateSettings above
	}
	if s.settings.maint != nil {
		s.settings.maint.SetConfig(e.Maintenance)
	}
	cfg.ApplyEditable(e)

	go s.RecheckCapabilities()

	var warnings []string
	if e.Auth.TrustedRole == "admin" {
		for _, n := range e.Auth.TrustedNetworks {
			if n == "0.0.0.0/0" || n == "::/0" {
				warnings = append(warnings, "every client that reaches xirc gets admin")
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, s.buildSettingsResponse(cfg, warnings))
}

func validateSettings(e config.Editable) []string {
	var errs []string
	for _, d := range []struct{ key, dir string }{
		{"storage.downloads_dir", e.Storage.DownloadsDir}, {"storage.temp_dir", e.Storage.TempDir},
	} {
		if !filepath.IsAbs(d.dir) {
			errs = append(errs, d.key+" must be an absolute path")
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
	return errs
}

// checkEnvLocks compares current vs submitted for every dotted key
// config.EnvLockedKeys() reports as locked, mapping the key to its Editable
// field by yaml tag (mirroring EnvLockedKeys' own reflection). nil and empty
// slices compare equal, so an unset list field submitted as [] isn't flagged.
func checkEnvLocks(current, submitted config.Editable) []string {
	var errs []string
	cv, sv := reflect.ValueOf(current), reflect.ValueOf(submitted)
	t := cv.Type()
	for i := 0; i < t.NumField(); i++ {
		sec := t.Field(i)
		secName := strings.Split(sec.Tag.Get("yaml"), ",")[0]
		cf, sf := cv.Field(i), sv.Field(i)
		for j := 0; j < sec.Type.NumField(); j++ {
			key := strings.Split(sec.Type.Field(j).Tag.Get("yaml"), ",")[0]
			envKey := "XIRC_" + strings.ToUpper(secName) + "_" + strings.ToUpper(key)
			if _, ok := os.LookupEnv(envKey); !ok {
				continue
			}
			a, b := cf.Field(j).Interface(), sf.Field(j).Interface()
			if valuesEqual(a, b) {
				continue
			}
			errs = append(errs, secName+"."+key+" is set by environment variable "+envKey+" and can't be changed here")
		}
	}
	return errs
}

func valuesEqual(a, b any) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if av.Kind() == reflect.Slice && bv.Kind() == reflect.Slice && av.Len() == 0 && bv.Len() == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
