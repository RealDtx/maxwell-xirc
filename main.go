package main

import (
	"bufio"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/fscheck"
	"github.com/RealDtx/maxwell-irc/internal/debug"
	"github.com/RealDtx/maxwell-irc/internal/exitcodes"
	ircpkg "github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/library"
	"github.com/RealDtx/maxwell-irc/maintenance"
	"github.com/RealDtx/maxwell-irc/notify"
	"github.com/RealDtx/maxwell-irc/parser"
	"github.com/RealDtx/maxwell-irc/queue"
	"github.com/RealDtx/maxwell-irc/server"
	wsPkg "github.com/RealDtx/maxwell-irc/ws"
)

//go:embed all:web
var embeddedWeb embed.FS

var version = "dev"

// checkDirectories probes storage.media_dir and storage.downloads_dir.
// Dirs that do not exist are returned as a slice — the caller handles them.
// Permission errors and other I/O failures are still fatal.
func checkDirectories(mediaDir, downloadsDir string) []string {
	seen := map[string]bool{}
	var bad []string
	for _, dir := range []string{mediaDir, downloadsDir} {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true

		probe := dir + "/.mxirc_write_check"
		f, err := os.Create(probe)
		if err == nil {
			f.Close()
			os.Remove(probe)
			log.Printf("startup: verified writable: %s", dir)
			continue
		}

		if os.IsPermission(err) || errors.Is(err, syscall.EROFS) {
			log.Printf("warning: %s — dependent features disabled until fixed (see /api/capabilities)", fscheck.Probe(dir).Reason)
			continue
		}
		if os.IsNotExist(err) {
			bad = append(bad, dir)
			continue
		}
		log.Printf("FATAL (transient): destination dir %q check failed: %v — will retry on restart (exit 1)", dir, err)
		os.Exit(exitcodes.ExitTransient)
	}
	return bad
}

// loadLibrary loads the library taxonomy config (categories.yaml, next to
// configPath by default). Missing: detect defaults from cfg.Storage.MediaDir
// and save them. Present but unparseable: log it and fall back to detected
// defaults in memory, without touching the file.
func loadLibrary(configPath string, cfg *config.Config) *library.Manager {
	path := cfg.Storage.CategoriesFile
	if path == "" {
		path = filepath.Join(filepath.Dir(configPath), "categories.yaml")
	}

	var libCfg library.Config
	if _, err := os.Stat(path); os.IsNotExist(err) {
		libCfg = library.Detect(cfg.Storage.MediaDir)
		if err := library.Save(path, &libCfg); err != nil {
			log.Printf("warning: failed to save detected library config to %s: %v", path, err)
		} else {
			log.Printf("library: detected default categories from %s, saved to %s", cfg.Storage.MediaDir, path)
		}
	} else if loaded, upgraded, err := library.Load(path); err != nil {
		log.Printf("warning: failed to parse %s: %v — using detected defaults (not saved)", path, err)
		libCfg = library.Detect(cfg.Storage.MediaDir)
	} else {
		libCfg = *loaded
		if upgraded {
			if err := library.Save(path, &libCfg); err != nil {
				log.Printf("warning: failed to save upgraded library config to %s: %v", path, err)
			} else {
				log.Printf("library: upgraded %s to version %d (series/movie now unpack tar/zip/rar/7z)", path, libCfg.Version)
			}
		}
	}
	return library.NewManager(path, libCfg)
}

// isTerminal reports whether stdin is an interactive terminal.
// Uses the TCGETS ioctl — only succeeds on real TTY file descriptors,
// unlike a stat-based check which also matches /dev/null.
func isTerminal() bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// runCLIWizard presents the interactive directory wizard on the terminal.
// Only call this when isTerminal() is true.
// It updates config.yaml, then returns.
func runCLIWizard(badDirs []string, state *server.SetupState) {
	suggestions := server.DefaultSuggestions(badDirs, state.DownloadsDir, state.HomeDir)

	fmt.Fprintf(os.Stderr, "\nmaxwell-irc: the following destination directories do not exist:\n\n")
	for _, dir := range badDirs {
		fmt.Fprintf(os.Stderr, "  %s\n    → used by: %s\n", dir, server.DirLabel(dir, state))
	}
	fmt.Fprintf(os.Stderr, "\nEnter replacement paths (press Enter to accept suggestion):\n\n")

	reader := bufio.NewReader(os.Stdin)
	var mappings []server.Mapping
	for _, dir := range badDirs {
		sug := suggestions[dir]
		fmt.Fprintf(os.Stderr, "  %s  [%s]: ", dir, sug)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			line = sug
		}
		mappings = append(mappings, server.Mapping{OldDir: dir, NewDir: line})
	}

	fmt.Fprintf(os.Stderr, "\nApply? [Y/n]: ")
	confirm, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(confirm)) == "n" {
		// User declined — apply pure defaults instead
		mappings = mappings[:0]
		for _, dir := range badDirs {
			mappings = append(mappings, server.Mapping{OldDir: dir, NewDir: suggestions[dir]})
		}
	}

	if err := server.ApplyMappings(mappings, state); err != nil {
		log.Printf("warning: wizard failed to apply mappings: %v", err)
	}
}

// runCreateAdmin reads the password from XIRC_ADMIN_PASSWORD, or prompts
// twice on a TTY (echo is visible; ponytail: add x/term for hidden input if wanted).
func runCreateAdmin(store db.Store, name string, in io.Reader, isTTY bool) error {
	pw := os.Getenv("XIRC_ADMIN_PASSWORD")
	if pw == "" {
		if !isTTY {
			return errors.New("set XIRC_ADMIN_PASSWORD or run interactively (docker exec -it)")
		}
		r := bufio.NewReader(in)
		fmt.Fprint(os.Stderr, "Password: ")
		a, _ := r.ReadString('\n')
		fmt.Fprint(os.Stderr, "Repeat:   ")
		b, _ := r.ReadString('\n')
		if strings.TrimRight(a, "\r\n") != strings.TrimRight(b, "\r\n") {
			return errors.New("passwords do not match")
		}
		pw = strings.TrimRight(a, "\r\n")
	}
	return server.CreateOrResetAdmin(store, name, pw)
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	debugFlag := flag.Bool("debug", false, "enable verbose debug logging")
	createAdmin := flag.String("create-admin", "", "create or reset an admin user, then exit (password from XIRC_ADMIN_PASSWORD or prompt)")
	flag.Parse()

	if *debugFlag {
		debug.Enabled = true
	}
	if debug.Enabled {
		log.Println("[DEBUG] debug logging enabled")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	dsn := cfg.Database.Path
	if cfg.Database.Driver == "mysql" {
		dsn = cfg.Database.DSN
	}

	store, err := db.NewStore(cfg.Database.Driver, dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	if *createAdmin != "" {
		if err := runCreateAdmin(store, *createAdmin, os.Stdin, isTerminal()); err != nil {
			log.Fatalf("create-admin: %v", err)
		}
		fmt.Printf("admin %q ready\n", strings.ToLower(strings.TrimSpace(*createAdmin)))
		return
	}

	auth, err := server.NewAuth(cfg.Auth, store, cfg.Server.Prefix)
	if err != nil {
		log.Printf("FATAL (config): %v (exit 78)", err)
		os.Exit(exitcodes.ExitConfig)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
		log.Printf("warning: could not determine home dir: %v", err)
	}

	badDirs := checkDirectories(cfg.Storage.MediaDir, cfg.Storage.DownloadsDir)

	setupState := &server.SetupState{
		Required:     len(badDirs) > 0,
		BadDirs:      badDirs,
		MediaDir:     cfg.Storage.MediaDir,
		DownloadsDir: cfg.Storage.DownloadsDir,
		HomeDir:      homeDir,
		ConfigPath:   *configPath,
	}

	if len(badDirs) > 0 {
		if isTerminal() {
			runCLIWizard(badDirs, setupState)
			// Reload storage paths from the updated config.yaml so the queue
			// engine and seed use the new dirs, not the pre-wizard bad paths.
			if newCfg, err := config.Load(*configPath); err == nil {
				cfg.Storage.MediaDir = newCfg.Storage.MediaDir
				cfg.Storage.DownloadsDir = newCfg.Storage.DownloadsDir
			}
		}
		// Non-TTY: setupState.Required stays true; web wizard will handle it.
	}

	// Seed default parse patterns
	if err := parser.SeedPatterns(store); err != nil {
		log.Printf("warning: failed to seed patterns: %v", err)
	}

	// Sync config-defined patterns to DB (skip if name already exists)
	if len(cfg.Patterns) > 0 {
		cfgPatterns := make([]parser.ConfigPattern, len(cfg.Patterns))
		for i, cp := range cfg.Patterns {
			cfgPatterns[i] = parser.ConfigPattern{
				Name:         cp.Name,
				Regex:        cp.Regex,
				FieldMapping: cp.FieldMapping,
				Priority:     cp.Priority,
				Tags:         cp.Tags,
			}
		}
		if _, err := parser.SyncPatterns(store, cfgPatterns); err != nil {
			log.Printf("warning: failed to sync config patterns: %v", err)
		}
	}

	bus := ircpkg.NewEventBus()
	browserNotifier := notify.NewBrowserNotifier(bus)
	_ = browserNotifier // Used by engine for explicit notifications

	msgBuf := ircpkg.NewMessageBuffer(bus, 1000)
	logDir := filepath.Join(filepath.Dir(cfg.Database.Path), "logs")
	os.MkdirAll(logDir, 0o755)
	if st := fscheck.Probe(logDir); st.Write {
		msgBuf.SetLogDir(logDir)
	} else {
		log.Printf("warning: channel logging disabled: %s", st.Reason)
	}
	msgBuf.Start()
	defer msgBuf.Stop()

	errBuf := ircpkg.NewErrorBuffer(bus, 200)
	errBuf.Start()
	defer errBuf.Stop()

	ircMgr := ircpkg.NewManager(store, bus)

	if err := ircMgr.LoadFromStore(); err != nil {
		log.Printf("warning: failed to load IRC servers: %v", err)
	}

	p := parser.New(store, bus)
	p.Start()

	libMgr := loadLibrary(*configPath, cfg)

	eng := queue.NewEngine(store, bus, ircMgr, &cfg.Storage, cfg.Downloads.MaxConcurrent)
	eng.SetLibrary(libMgr)
	eng.Start()

	maint := maintenance.New(store, cfg.Maintenance)
	maint.Start()

	hub := wsPkg.NewHub(bus)
	hub.Start()

	ircMgr.ConnectAutoConnect()

	webFS, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("embedded web FS: %v", err)
	}
	srv := server.New(store, ircMgr, p, eng, hub, msgBuf, errBuf, setupState, cfg.Server.Prefix, webFS)
	srv.SetLibrary(libMgr)
	srv.SetDownloadsDir(cfg.Storage.DownloadsDir)
	srv.SetAuth(auth)

	srv.SetCapabilityInputs(cfg.Storage.TempDir, logDir, bus)
	caps := srv.RecheckCapabilities()
	for _, c := range []struct {
		name string
		cap  server.Capability
	}{{"downloads", caps.Downloads}, {"library", caps.Library}, {"library config", caps.LibraryConfig}} {
		if !c.cap.OK {
			log.Printf("warning: %s disabled: %s", c.name, c.cap.Reason)
		}
	}
	capStop := make(chan struct{})
	srv.StartCapabilityRefresh(5*time.Minute, capStop)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		close(capStop)
		p.Stop() // before httpServer.Close: main exits once ListenAndServe returns
		httpServer.Close()
		ircMgr.Shutdown()
		hub.Stop()
		eng.Stop()
		maint.Stop()
	}()

	log.Printf("maxwell-irc starting on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
