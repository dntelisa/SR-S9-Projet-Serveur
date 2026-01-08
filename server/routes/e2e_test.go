package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dntelisa/SR-S9-Projet-Serveur/server/game"
	"github.com/gorilla/websocket"
)

// TestE2E_MultiClientCollect launches a test server and two Go "headless" clients
// that connect via websocket, perform controlled moves and validate that the
// collected event is broadcast to all clients and the server state is updated.
func TestE2E_MultiClientCollect(t *testing.T) {
	// create deterministic small game
	g := game.NewGame(5, 5, 0)
	g.ClearSweets()
	g.SetSweet("s1", 2, 2)
	g.Start(100)
	game.Default = g
	// forward this test game broadcasts to the hub so it delivers to connected clients
	go func() {
		for b := range g.StateBroadcast {
			h.broadcast <- b
		}
	}()
	go func() {
		for b := range g.EventBroadcast {
			h.broadcast <- b
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", Root)
	mux.HandleFunc("/ws", WS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	// helper to dial and join
	dialJoin := func(name string) (*websocket.Conn, string) {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial %s: %v", name, err)
		}
		join := map[string]interface{}{"type": "join", "name": name}
		b, _ := jsonMarshal(join)
		if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
			t.Fatalf("write join %s: %v", name, err)
		}
		// read join_ack
		id := readJoinAck(t, c)
		return c, id
	}

	c1, id1 := dialJoin("A")
	defer c1.Close()
	c2, id2 := dialJoin("B")
	defer c2.Close()

	// position players around the sweet
	g.SetPlayerPosition(id1, 1, 2) // left
	g.SetPlayerPosition(id2, 3, 2) // right

	// c2 moves left then c1 moves right, c2 should collect
	moveL := map[string]interface{}{"type": "move", "dir": "left"}
	mbL, _ := jsonMarshal(moveL)
	moveR := map[string]interface{}{"type": "move", "dir": "right"}
	mbR, _ := jsonMarshal(moveR)

	// send moves
	if err := c2.WriteMessage(websocket.TextMessage, mbL); err != nil {
		t.Fatalf("c2 write move: %v", err)
	}
	// tiny sleep to ensure arrival order
	time.Sleep(5 * time.Millisecond)
	if err := c1.WriteMessage(websocket.TextMessage, mbR); err != nil {
		t.Fatalf("c1 write move: %v", err)
	}

	// each client should receive an 'event' collected message
	checkEvent := func(c *websocket.Conn) {
		deadline := time.Now().Add(1000 * time.Millisecond)
		for time.Now().Before(deadline) {
			c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			_, msg, err := c.ReadMessage()
			if err != nil {
				t.Fatalf("read error while waiting for event: %v", err)
			}
			var m map[string]interface{}
			if err := jsonUnmarshal(msg, &m); err != nil {
				continue
			}
			if m["type"] == "event" && m["event"] == "collected" {
				return
			}
		}
		t.Fatalf("client did not receive collected event")
	}

	checkEvent(c1)
	checkEvent(c2)

	// verify server state
	p1 := g.GetPlayer(id1)
	p2 := g.GetPlayer(id2)
	if !(p2.Score == 1 && g.SweetsCount() == 0) {
		t.Fatalf("unexpected server state after collect: p1=%+v p2=%+v sweets=%d", p1, p2, g.SweetsCount())
	}
}

// TestE2E_ClientReconnect verifies that the headless C++ client will reconnect
// automatically when the server stops and restarts on the same port.
func TestE2E_ClientReconnect(t *testing.T) {
	// locate client binary
	clientPath := filepath.Join("..", "SR-S9-Projet-Client", "build", "srclient")
	if _, err := os.Stat(clientPath); err != nil {
		t.Skipf("client binary not found at %s: %v", clientPath, err)
	}

	// prepare server handler
	mux := http.NewServeMux()
	mux.HandleFunc("/", Root)
	mux.HandleFunc("/ws", WS)

	// helper to start server on a given listener
	startServer := func(l net.Listener) *http.Server {
		s := &http.Server{Handler: mux}
		go func() { _ = s.Serve(l) }()
		return s
	}

	// pick a free port by listening on :0
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	srv := startServer(l)
	defer func() { _ = srv.Close() }()

	wsURL := "ws://127.0.0.1:" + strconv.Itoa(port) + "/ws"

	// start client process
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, clientPath, "--headless", "--server="+wsURL, "--name=e2e-client")
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	// read combined output lines
	lines := make(chan string, 64)
	scanOut := func(r io.Reader) {
		s := bufio.NewScanner(r)
		for s.Scan() {
			lines <- s.Text()
		}
	}
	go scanOut(stdout)
	go scanOut(stderr)

	// wait for first join_ack
	found := waitForOutput(lines, "join_ack", 3*time.Second)
	if !found {
		cmd.Process.Kill()
		t.Fatalf("did not see initial join_ack from client")
	}

	// stop server (simulate crash)
	if err := srv.Close(); err != nil {
		t.Fatalf("shutdown server: %v", err)
	}

	// allow client to notice
	time.Sleep(200 * time.Millisecond)

	// start a new server on the same port (retry until available)
	var l2 net.Listener
	for i := 0; i < 20; i++ {
		l2, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		cmd.Process.Kill()
		t.Fatalf("could not bind to same port: %v", err)
	}
	srv2 := startServer(l2)
	defer func() { _ = srv2.Close() }()

	// wait for the client to reconnect (see second join_ack)
	found2 := waitForOutput(lines, "join_ack", 6*time.Second)
	if !found2 {
		cmd.Process.Kill()
		t.Fatalf("client did not reconnect after server restart")
	}

	_ = cmd.Process.Kill()
}

// helper: simple json marshal/unmarshal wrappers so we don't import encoding/json repeatedly
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
func jsonUnmarshal(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}

// waitForOutput returns true if any line read from the channel contains substr within timeout
func waitForOutput(lines <-chan string, substr string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case l := <-lines:
			if strings.Contains(l, substr) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
