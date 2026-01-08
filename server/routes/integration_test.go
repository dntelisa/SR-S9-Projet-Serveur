package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/dntelisa/SR-S9-Projet-Serveur/server/game"
)

func readJoinAck(t *testing.T, c *websocket.Conn) string {
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, msg, err := c.ReadMessage()
		if err != nil {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(msg, &m); err != nil {
			continue
		}
		if m["type"] == "join_ack" {
			if id, ok := m["id"].(string); ok {
				return id
			}
		}
	}
	t.Fatalf("no join_ack received")
	return ""
}

func TestIntegrationConflictViaWS(t *testing.T) {
	// create deterministic small game
	g := game.NewGame(3, 3, 0)
	// set fixed sweet at (1,1)
	g.ClearSweets()
	g.SetSweet("s1", 1, 1)
	// start fast ticks
	g.Start(100)
	// replace global default
	game.Default = g

	// register routes on a dedicated mux and start test server
	mux := http.NewServeMux()
	mux.HandleFunc("/", Root)
	mux.HandleFunc("/ws", WS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	// connect two clients
	c1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial c1: %v", err)
	}
	defer c1.Close()
	c2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial c2: %v", err)
	}
	defer c2.Close()

	// join both
	join1 := map[string]interface{}{"type": "join", "name": "A"}
	b1, _ := json.Marshal(join1)
	if err := c1.WriteMessage(websocket.TextMessage, b1); err != nil {
		t.Fatalf("c1 write join: %v", err)
	}
	join2 := map[string]interface{}{"type": "join", "name": "B"}
	b2, _ := json.Marshal(join2)
	if err := c2.WriteMessage(websocket.TextMessage, b2); err != nil {
		t.Fatalf("c2 write join: %v", err)
	}

	id1 := readJoinAck(t, c1)
	id2 := readJoinAck(t, c2)

	// Place the players adjacent to sweet: p1 at (0,1) left, p2 at (2,1) right
	g.SetPlayerPosition(id1, 0, 1)
	g.SetPlayerPosition(id2, 2, 1)

	// send both moves simultaneously towards (1,1)
	moveL := map[string]interface{}{"type": "move", "dir": "right"}
	mbL, _ := json.Marshal(moveL)
	moveR := map[string]interface{}{"type": "move", "dir": "left"}
	mbR, _ := json.Marshal(moveR)

	// send moves in controlled order: c2 first, then c1 (so c2 should win)
	if err := c2.WriteMessage(websocket.TextMessage, mbR); err != nil {
		t.Fatalf("c2 write move: %v", err)
	}
	// tiny sleep to ensure arrival order
	time.Sleep(5 * time.Millisecond)
	if err := c1.WriteMessage(websocket.TextMessage, mbL); err != nil {
		t.Fatalf("c1 write move: %v", err)
	}

	// wait a short while for tick processing
	time.Sleep(50 * time.Millisecond)

	// check result: since c2's command arrived first, id2 should collect
	p1 := g.GetPlayer(id1)
	p2 := g.GetPlayer(id2)
	sweetsLeft := g.SweetsCount()

	if !(p2.Score == 1 && p2.X == 1 && p1.Score == 0 && p1.X == 0 && sweetsLeft == 0) {
		t.Fatalf("unexpected outcome: p1=%+v p2=%+v sweets=%d", p1, p2, sweetsLeft)
	}
}

func TestIntegrationCollectedEventReceived(t *testing.T) {
	// create game
	g := game.NewGame(3, 3, 0)
	g.ClearSweets()
	g.Start(100)
	game.Default = g
	// forward this test game broadcasts to the hub so the test server receives them
	go func() { for b := range g.StateBroadcast { h.broadcast <- b } }()
	go func() { for b := range g.EventBroadcast { h.broadcast <- b } }()

	mux := http.NewServeMux()
	mux.HandleFunc("/", Root)
	mux.HandleFunc("/ws", WS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// join
	join := map[string]interface{}{"type": "join", "name": "Tester"}
	b, _ := json.Marshal(join)
	if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("write join: %v", err)
	}
	id := readJoinAck(t, c)

	// place sweet next to player (right if possible, otherwise left)
	p := g.GetPlayer(id)
	sx := p.X + 1
	dir := "right"
	if sx >= g.W {
		sx = p.X - 1
		dir = "left"
	}
	g.SetSweet("s1", sx, p.Y)

	// send move towards the sweet
	move := map[string]interface{}{"type": "move", "dir": dir}
	mb, _ := json.Marshal(move)
	if err := c.WriteMessage(websocket.TextMessage, mb); err != nil {
		t.Fatalf("write move: %v", err)
	}

	// wait for collected event (fail on read errors)
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("collected event not received")
		}
		c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, msg, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(msg, &m); err != nil {
			continue
		}
		if m["type"] == "event" && m["event"] == "collected" {
			if m["player"] != id || m["sweet"] != "s1" {
				t.Fatalf("unexpected event content: %v", m)
			}
			return
		}
	}
}
