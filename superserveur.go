package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/dntelisa/SR-S9-Projet-Serveur/server"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

func main() {
	flag.Parse()
	log.Println("[INFO] SUPERSERVEUR")
	log.Println("[INFO] Waiting for requests...")
	server.SetupRoutes()
	log.Fatal(http.ListenAndServe(*addr, nil))
}
