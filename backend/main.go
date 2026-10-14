package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Josh-TX/notepadtt/internal/config"
	"github.com/Josh-TX/notepadtt/internal/server"
	"github.com/Josh-TX/notepadtt/internal/webui"
)

var version = "dev" // set via -ldflags "-X main.version=..."

func main() {
	cfg, err := config.Load(os.Args[1:], version)
	if err != nil {
		log.Fatal(err)
	}
	dir, port := cfg.Directory, cfg.Port
	if !webui.Embedded() {
		log.Printf("no frontend embedded, use the Vite dev server (or build with -tags embed_frontend)")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		log.Fatalf("directory %q does not exist or is not a directory", dir)
	}

	srv, err := server.NewServer(dir, webui.FS())
	if err != nil {
		log.Fatal(err)
	}
	addr := fmt.Sprintf(":%d", port)
	log.Printf("listening on %s, serving %s", addr, dir)
	httpSrv := &http.Server{Addr: addr, Handler: srv, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
	}()
	if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	srv.FlushAll() // write-behind: don't lose the last couple of seconds of edits
}
