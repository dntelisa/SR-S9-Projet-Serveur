package routes

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dntelisa/SR-S9-Projet-Serveur/server/game"
)

// Launch 50 clients in parallel to test server load.
// It simply checks that the server survives and handles connections.
func TestChaos_MonkeyCrowd(t *testing.T) {
	// Locate client binary
	clientPath := filepath.Join("..", "..", "..", "SR-S9-Projet-Client", "build", "srclient")	
	if _, err := os.Stat(clientPath); err != nil {
		t.Skipf("Client binaire non trouvé (chaos test ignoré): %v", err)
	}

	// Start test server on a random port
	game.Default = game.NewGame(20, 20, 50) 
	game.Default.Start(10)

	// Forward game broadcasts to hub
	go func() {
		for b := range game.Default.StateBroadcast {
			h.broadcast <- b
		}
	}()
	go func() {
		for b := range game.Default.EventBroadcast {
			h.broadcast <- b
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", Root)
	mux.HandleFunc("/ws", WS)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	wsURL := "ws://127.0.0.1:" + strconv.Itoa(port) + "/ws"

	srv := &http.Server{Handler: mux}
	go func() { srv.Serve(l) }()
	defer srv.Close()

	t.Logf("Serveur de Chaos démarré sur %s", wsURL)

	// Launch 50 headless clients
	const NumClients = 50
	var wg sync.WaitGroup
	var activeCmds []*exec.Cmd
	var mu sync.Mutex

	t.Logf("Lancement de %d clients headless...", NumClients)

	for i := 0; i < NumClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("monkey-%d", id)
			
			// Command to launch client
			cmd := exec.Command(clientPath, "--headless", "--server="+wsURL, "--name="+name)
			
			mu.Lock()
			activeCmds = append(activeCmds, cmd)
			mu.Unlock()

			if err := cmd.Start(); err != nil {
				t.Errorf("Erreur lancement client %d: %v", id, err)
				return
			}
			
			// Wait for client to finish
			cmd.Wait()
		}(i)
		
		// Slight delay to avoid overwhelming the server instantly
		time.Sleep(10 * time.Millisecond)
	}

	// Wait a bit for all clients to start
	time.Sleep(2 * time.Second)

	// Cleanup: Terminate all clients
	mu.Lock()
	for _, cmd := range activeCmds {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}
	mu.Unlock()

	// Wait for all goroutines
	t.Log("Vérification de la survie du serveur...")
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("Le serveur semble mort après le chaos test: %v", err)
	}
	t.Log("Succès : Le serveur a survécu à la foule !")
}