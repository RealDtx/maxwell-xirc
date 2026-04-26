package main

import (
	"bufio"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/internal/debug"
	"github.com/RealDtx/maxwell-irc/internal/exitcodes"
	ircpkg "github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/notify"
	"github.com/RealDtx/maxwell-irc/parser"
	"github.com/RealDtx/maxwell-irc/queue"
	"github.com/RealDtx/maxwell-irc/routing"
	"github.com/RealDtx/maxwell-irc/server"
	wsPkg "github.com/RealDtx/maxwell-irc/ws"
)

//go:embed all:web
var embeddedWeb embed.FS

var version = "dev"

// checkDirectories probes each unique destination_dir in the routing rules.
// Dirs that do not exist are returned as a slice — the caller handles them.
// Permission errors and other I/O failures are still fatal.
func checkDirectories(store db.Store) []string {
	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		log.Printf("warning: could not load routing rules for dir check: %v", err)
		return nil
	}

	seen := map[string]bool{}
	var bad []string
	for _, r := range rules {
		if r.DestinationDir == "" || seen[r.DestinationDir] {
			continue
		}
		seen[r.DestinationDir] = true

		probe := r.DestinationDir + "/.mxirc_write_check"
		f, err := os.Create(probe)
		if err == nil {
			f.Close()
			os.Remove(probe)
			log.Printf("startup: verified writable: %s", r.DestinationDir)
			continue
		}

		if os.IsPermission(err) || errors.Is(err, syscall.EROFS) {
			log.Printf("FATAL (config): destination dir %q not writable — fix ownership or ACL, then restart (exit 78)", r.DestinationDir)
			os.Exit(exitcodes.ExitConfig)
		}
		if os.IsNotExist(err) {
			bad = append(bad, r.DestinationDir)
			continue
		}
		log.Printf("FATAL (transient): destination dir %q check failed: %v — will retry on restart (exit 1)", r.DestinationDir, err)
		os.Exit(exitcodes.ExitTransient)
	}
	return bad
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
// It updates config.yaml and the DB, then returns.
func runCLIWizard(badDirs []string, store db.Store, state *server.SetupState) {
	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		log.Printf("warning: could not load rules for wizard: %v", err)
		rules = nil
	}
	suggestions := server.DefaultSuggestions(badDirs, rules, state.HomeDir)

	fmt.Fprintf(os.Stderr, "\nmaxwell-irc: the following destination directories do not exist:\n\n")
	for _, dir := range badDirs {
		patterns := server.PatternsForDir(rules, dir)
		fmt.Fprintf(os.Stderr, "  %s\n    → used by: %s\n", dir, strings.Join(patterns, " "))
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

	if err := server.ApplyMappings(mappings, store, state); err != nil {
		log.Printf("warning: wizard failed to apply mappings: %v", err)
	}
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	debugFlag := flag.Bool("debug", false, "enable verbose debug logging")
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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
		log.Printf("warning: could not determine home dir: %v", err)
	}

	// Sync DB routing rules to match config.yaml BEFORE checking directories,
	// so that a changed config is reflected in checkDirectories immediately.
	if err := routing.SyncBuiltinRuleDirs(store, cfg.Storage.MediaDir, cfg.Storage.DownloadsDir); err != nil {
		log.Printf("warning: failed to sync builtin routing rules: %v", err)
	}

	badDirs := checkDirectories(store)

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
			runCLIWizard(badDirs, store, setupState)
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

	if err := routing.SeedRoutingRules(store, cfg.Storage.MediaDir, cfg.Storage.DownloadsDir); err != nil {
		log.Printf("warning: failed to seed routing rules: %v", err)
	}

	bus := ircpkg.NewEventBus()
	browserNotifier := notify.NewBrowserNotifier(bus)
	_ = browserNotifier // Used by engine for explicit notifications

	msgBuf := ircpkg.NewMessageBuffer(bus, 1000)
	msgBuf.SetLogDir(filepath.Dir(cfg.Database.Path) + "/logs")
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

	eng := queue.NewEngine(store, bus, ircMgr, &cfg.Storage, cfg.Downloads.MaxConcurrent)
	eng.Start()

	hub := wsPkg.NewHub(bus)
	hub.Start()

	ircMgr.ConnectAutoConnect()

	webFS, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("embedded web FS: %v", err)
	}
	srv := server.New(store, ircMgr, p, eng, hub, msgBuf, errBuf, setupState, cfg.Server.Prefix, webFS)

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
		httpServer.Close()
		p.Stop()
		ircMgr.Shutdown()
		hub.Stop()
		eng.Stop()
	}()

	log.Printf("maxwell-irc starting on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
