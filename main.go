package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
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

	srv := server.New(store)

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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("xirc starting on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
