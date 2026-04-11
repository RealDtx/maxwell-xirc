package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/internal/exitcodes"
	ircpkg "github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/notify"
	"github.com/maxwell-xirc/xirc/parser"
	"github.com/maxwell-xirc/xirc/queue"
	"github.com/maxwell-xirc/xirc/routing"
	"github.com/maxwell-xirc/xirc/server"
	wsPkg "github.com/maxwell-xirc/xirc/ws"
)

var version = "dev"

func checkDirectories(store db.Store) {
	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		log.Printf("warning: could not load routing rules for dir check: %v", err)
		return
	}

	seen := map[string]bool{}
	for _, r := range rules {
		if r.DestinationDir == "" || seen[r.DestinationDir] {
			continue
		}
		seen[r.DestinationDir] = true

		probe := r.DestinationDir + "/.xirc_write_check"
		f, err := os.Create(probe)
		if err == nil {
			f.Close()
			os.Remove(probe)
			log.Printf("startup: verified writable: %s", r.DestinationDir)
			continue
		}

		if os.IsPermission(err) || errors.Is(err, syscall.EROFS) {
			log.Printf("FATAL (config): destination dir %q not writable by xirc user — fix ownership or ACL, then restart (exit 78)", r.DestinationDir)
			os.Exit(exitcodes.ExitConfig)
		}
		if os.IsNotExist(err) {
			log.Printf("FATAL (config): destination dir %q does not exist — create it and grant access, then restart (exit 78)", r.DestinationDir)
			os.Exit(exitcodes.ExitConfig)
		}
		log.Printf("FATAL (transient): destination dir %q check failed: %v — will retry on restart (exit 1)", r.DestinationDir, err)
		os.Exit(exitcodes.ExitTransient)
	}
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

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

	checkDirectories(store)

	// Seed default parse patterns
	if err := parser.SeedPatterns(store); err != nil {
		log.Printf("warning: failed to seed patterns: %v", err)
	}

	if err := routing.SeedRoutingRules(store, cfg.Storage.MediaDir, cfg.Storage.DownloadsDir); err != nil {
		log.Printf("warning: failed to seed routing rules: %v", err)
	}

	bus := ircpkg.NewEventBus()
	browserNotifier := notify.NewBrowserNotifier(bus)
	_ = browserNotifier // Used by engine for explicit notifications

	msgBuf := ircpkg.NewMessageBuffer(bus, 1000)
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

	eng := queue.NewEngine(store, bus, &cfg.Storage, cfg.Downloads.MaxConcurrent)
	eng.Start()

	hub := wsPkg.NewHub(bus)
	hub.Start()

	ircMgr.ConnectAutoConnect()

	srv := server.New(store, ircMgr, p, eng, hub, msgBuf, errBuf)

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

	log.Printf("xirc starting on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
