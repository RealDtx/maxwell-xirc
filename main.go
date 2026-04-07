package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
	ircpkg "github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
	"github.com/maxwell-xirc/xirc/queue"
	"github.com/maxwell-xirc/xirc/server"
)

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

	// Seed default parse patterns
	if err := parser.SeedPatterns(store); err != nil {
		log.Printf("warning: failed to seed patterns: %v", err)
	}

	bus := ircpkg.NewEventBus()
	ircMgr := ircpkg.NewManager(store, bus)

	if err := ircMgr.LoadFromStore(); err != nil {
		log.Printf("warning: failed to load IRC servers: %v", err)
	}

	p := parser.New(store, bus)
	p.Start()

	eng := queue.NewEngine(store, bus, &cfg.Storage, cfg.Downloads.MaxConcurrent)
	eng.Start()

	ircMgr.ConnectAutoConnect()

	srv := server.New(store, ircMgr, p, eng)

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
		p.Stop()
		eng.Stop()
		ircMgr.Shutdown()
		httpServer.Close()
	}()

	log.Printf("xirc starting on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
