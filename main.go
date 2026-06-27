package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"

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

	srv, err := backend.NewServer(dir, frontendDist)
	if err != nil {
		log.Fatal(err)
	}
	addr := fmt.Sprintf(":%d", port)
	log.Printf("listening on %s, serving %s", addr, dir)
	log.Fatal(http.ListenAndServe(addr, srv))
}
