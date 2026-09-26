package server

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/RealDtx/maxwell-irc/fscheck"
	"github.com/RealDtx/maxwell-irc/irc"
)

type Capability struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

type Capabilities struct {
	Downloads     Capability          `json:"downloads"`
	Library       Capability          `json:"library"`
	LibraryRead   Capability          `json:"library_read"`
	LibraryConfig Capability          `json:"library_config"`
	Logging       Capability          `json:"logging"`
	Roots         []fscheck.DirStatus `json:"roots"`
	Docker        bool                `json:"docker"`
}

type capState struct {
	mu      sync.Mutex
	tempDir string
	logDir  string
	bus     *irc.EventBus
	current *Capabilities
}

// SetCapabilityInputs wires the paths and event bus capability checks need —
// set once at startup, alongside SetDownloadsDir/SetLibrary.
func (s *Server) SetCapabilityInputs(tempDir, logDir string, bus *irc.EventBus) {
	s.caps.mu.Lock()
	s.caps.tempDir, s.caps.logDir, s.caps.bus = tempDir, logDir, bus
	s.caps.mu.Unlock()
}

// writableOrCreatable: an existing dir must be writable; a missing one is
// fine when its nearest existing parent is (the engine MkdirAlls it).
func writableOrCreatable(dir string) Capability {
	for d := dir; ; d = filepath.Dir(d) {
		st := fscheck.Probe(d)
		if st.Exists {
			return Capability{OK: st.Write, Reason: st.Reason}
		}
		if filepath.Dir(d) == d {
			return Capability{Reason: st.Reason}
		}
	}
}

func firstFailure(dirs ...string) Capability {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if c := writableOrCreatable(d); !c.OK {
			return c
		}
	}
	return Capability{OK: true}
}

func (s *Server) computeCapabilities() Capabilities {
	s.caps.mu.Lock()
	tempDir, logDir := s.caps.tempDir, s.caps.logDir
	s.caps.mu.Unlock()

	c := Capabilities{Roots: []fscheck.DirStatus{}}
	_, err := os.Stat("/.dockerenv")
	c.Docker = err == nil

	for _, r := range s.configuredRoots() {
		c.Roots = append(c.Roots, fscheck.Probe(r))
	}
	c.Downloads = firstFailure(s.downloadsDir, tempDir)
	c.Logging = firstFailure(logDir)
	c.Library, c.LibraryRead, c.LibraryConfig = Capability{OK: true}, Capability{OK: true}, Capability{OK: true}
	if s.library != nil {
		cfg := s.library.Get()
		var dirs []string
		dirs = append(dirs, cfg.MediaRoot)
		for _, cat := range cfg.Categories {
			if !cat.Enabled || cat.Dir == "" {
				continue
			}
			d := cat.Dir
			if !filepath.IsAbs(d) {
				d = filepath.Join(cfg.MediaRoot, d)
			}
			dirs = append(dirs, d)
		}
		c.Library = firstFailure(dirs...)
		if st := fscheck.Probe(cfg.MediaRoot); !st.Read {
			c.LibraryRead = Capability{Reason: st.Reason}
		}
		c.LibraryConfig = firstFailure(filepath.Dir(s.library.Path()))
	}
	return c
}

// RecheckCapabilities recomputes the snapshot, stores it, and — if it
// changed — publishes it on the event bus.
func (s *Server) RecheckCapabilities() Capabilities {
	c := s.computeCapabilities()
	s.caps.mu.Lock()
	changed := s.caps.current == nil || !reflect.DeepEqual(*s.caps.current, c)
	s.caps.current = &c
	bus := s.caps.bus
	s.caps.mu.Unlock()
	if changed && bus != nil {
		bus.Publish(irc.Event{Type: irc.EventCapabilities, Data: c})
	}
	return c
}

// capabilities returns the current snapshot, computing one first if none
// exists yet.
func (s *Server) capabilities() Capabilities {
	s.caps.mu.Lock()
	cur := s.caps.current
	s.caps.mu.Unlock()
	if cur == nil {
		return s.RecheckCapabilities()
	}
	return *cur
}

// StartCapabilityRefresh recomputes capabilities on a timer until stop is
// closed, so a fixed permission problem clears itself without a restart.
func (s *Server) StartCapabilityRefresh(every time.Duration, stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.RecheckCapabilities()
			case <-stop:
				return
			}
		}
	}()
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.capabilities())
}

func (s *Server) handleCapabilitiesRecheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.RecheckCapabilities())
}
