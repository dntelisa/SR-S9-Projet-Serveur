package main

import (
	"flag" // library for command-line flag parsing
	"log" // print logs
	"net/http" // HTTP server

	"github.com/dntelisa/SR-S9-Projet-Serveur/server"
)

// addr var is the address to listen on, default localhost:8080
var addr = flag.String("addr", "localhost:8080", "http service address")

func main() {
	flag.Parse() // address entry in terminal to replace default
	log.Println("[INFO] SUPERSERVEUR")
	log.Println("[INFO] Waiting for requests...")
	server.SetupRoutes()
	log.Fatal(http.ListenAndServe(*addr, nil))
}
