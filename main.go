package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Josh-TX/notepadtt/backend"
)

//go:embed frontend/dist
var frontendDist embed.FS

func main() {
	var dir string
	var port int
	flag.StringVar(&dir, "directory", ".", "root directory to serve")
	flag.StringVar(&dir, "d", ".", "root directory to serve")
	flag.IntVar(&port, "port", 8080, "port to listen on")
	flag.IntVar(&port, "p", 8080, "port to listen on")
	flag.Parse()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		log.Fatalf("directory %q does not exist or is not a directory", dir)
	}

	srv, err := backend.NewServer(dir, frontendDist)
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
