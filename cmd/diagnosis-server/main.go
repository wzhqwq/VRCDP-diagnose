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

	"github.com/wzhqwq/VRCDancePreloader/diagnose"
	"github.com/wzhqwq/VRCDancePreloader/internal/persistence"
)

func main() {
	dbPath := flag.String("db", "diagnose.db", "path to the diagnostics SQLite database file")
	addr := flag.String("addr", "localhost:7653", "HTTP listen address")
	prefix := flag.String("prefix", "/diagnosis/", "URL prefix for the diagnostics UI and API")
	startSession := flag.Bool("start-session", false, "create a new diagnostics session on startup")
	flag.Parse()

	if *dbPath == "" {
		log.Fatal("missing -db path")
	}

	if err := persistence.InitDiagnoseDB(*dbPath); err != nil {
		log.Fatalf("open diagnostics database: %v", err)
	}
	defer persistence.CloseDiagnoseDB()

	manager := diagnose.NewManager(diagnose.Config{
		SessionLabel:             "standalone-diagnosis-server",
		ChunkLoggingEnabled:      true,
		WindowAggregationEnabled: true,
		QueueSize:                4096,
		DropOnOverflow:           true,
		WindowSizes: []time.Duration{
			50 * time.Millisecond,
			100 * time.Millisecond,
		},
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *startSession {
		if err := manager.Start(ctx); err != nil {
			log.Fatalf("start diagnostics manager: %v", err)
		}
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown diagnostics manager: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mountDiagnostics(mux, *prefix, manager.HTTPHandler())

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("diagnostics UI: http://%s%s", *addr, cleanPrefix(*prefix))
		log.Printf("database: %s", *dbPath)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown server: %v", err)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}
}

func mountDiagnostics(mux *http.ServeMux, prefix string, handler http.Handler) {
	prefix = cleanPrefix(prefix)
	trimmed := prefix[:len(prefix)-1]

	mux.Handle(trimmed, http.RedirectHandler(prefix, http.StatusMovedPermanently))
	mux.Handle(prefix, http.StripPrefix(trimmed, handler))
}

func cleanPrefix(prefix string) string {
	if prefix == "" {
		return "/diagnosis/"
	}
	if prefix[0] != '/' {
		prefix = "/" + prefix
	}
	if prefix[len(prefix)-1] != '/' {
		prefix += "/"
	}
	return prefix
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), "Usage: diagnosis-server -db <diagnose.sqlite>\n\n")
	flag.PrintDefaults()
}

func init() {
	flag.Usage = usage
}
