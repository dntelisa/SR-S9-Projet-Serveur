package server

import (
	"net/http"

	"github.com/dntelisa/SR-S9-Projet-Serveur/server/routes"
)

// SetupRoutes Set up the server's routes.
func SetupRoutes() {
	http.HandleFunc("/", routes.Root)
	http.HandleFunc("/ws", routes.WS)
}
