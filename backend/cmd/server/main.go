package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zeusangis/dendrite/internal/activity"
	"github.com/zeusangis/dendrite/internal/api"
	"github.com/zeusangis/dendrite/internal/graph"
	"github.com/zeusangis/dendrite/internal/notes"
	"github.com/zeusangis/dendrite/internal/storage"
)

func main() {
	root := rootDir()
	notesDir := filepath.Join(root, "notes")
	dbPath := filepath.Join(root, "data", "dendrite.db")

	db, err := storage.OpenDatabase(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	vault := &notes.Vault{Root: notesDir}
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		log.Fatalf("create notes dir: %v", err)
	}

	service := graph.NewService(vault, db)
	collector, err := activity.New(db, service, activity.NativeSampler{}, os.Getenv("DENDRITE_ACTIVITY_AUTOSTART") != "false")
	if err != nil {
		log.Fatalf("activity collector: %v", err)
	}
	if os.Getenv("DENDRITE_ACTIVITY_AUTOSTART") == "false" {
		cfg := collector.Status().Config
		cfg.Enabled = false
		if err = collector.Configure(cfg); err != nil {
			log.Fatal(err)
		}
	}
	handler := &api.Handler{Service: service, Vault: vault, DB: db, Activity: collector}

	// Initial sync at startup.
	if _, err := service.Sync(); err != nil {
		log.Printf("initial sync failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go collector.Run(ctx)
	log.Printf("Activity tracking enabled: %t (pause in the UI)", collector.Status().Config.Enabled)
	// Periodic rescan so external edits to Markdown files are picked up.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := service.Sync(); err != nil {
					log.Printf("background sync: %v", err)
				}
			}
		}
	}()

	addr := "127.0.0.1:8080"
	if p := os.Getenv("DENDRITE_ADDR"); p != "" {
		addr = p
	}
	log.Printf("Dendrite server listening on %s", addr)
	log.Printf("Notes vault: %s", notesDir)
	server := &http.Server{Addr: addr, Handler: handler.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("server failed: %v", err)
	}
}

// rootDir resolves the project root: backend/ when run from there, repo root
// when run from the checkout root.
func rootDir() string {
	if root := os.Getenv("DENDRITE_ROOT"); root != "" {
		return root
	}
	if _, err := os.Stat("go.mod"); err == nil {
		return ".."
	}
	if _, err := os.Stat("notes"); err == nil {
		return "."
	}
	if _, err := os.Stat("../notes"); err == nil {
		return ".."
	}
	return "."
}
