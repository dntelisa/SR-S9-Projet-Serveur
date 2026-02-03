package routes

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/dntelisa/SR-S9-Projet-Serveur/server/game"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Client represents a websocket client connection.
type Client struct {
	conn     *websocket.Conn
	send     chan []byte
	playerID string
}

// Hub maintains the set of active clients and broadcasts messages to them.
type Hub struct {
	clients    map[*Client]bool // list of connected clients
	broadcast  chan []byte // messages to broadcast to all clients
	register   chan *Client // queue for registering new clients
	unregister chan *Client // queue for unregistering clients
	mu         sync.Mutex
}

var h = Hub{
	clients:    make(map[*Client]bool),
	broadcast:  make(chan []byte),
	register:   make(chan *Client),
	unregister: make(chan *Client),
}

func init() {
	go h.run()
	// forward game state to hub broadcast
	go func() {
		for b := range game.Default.StateBroadcast {
			h.broadcast <- b
		}
	}()
	// forward game events to hub broadcast (collected etc.)
	go func() {
		for b := range game.Default.EventBroadcast {
			h.broadcast <- b
		}
	}()
}

// Manage the hub: register/unregister clients and broadcast messages.
func (hub *Hub) run() {
	for {
		select {
		// New client registration, add his connection
		case c := <-hub.register:
			hub.mu.Lock()
			hub.clients[c] = true
			hub.mu.Unlock()
			log.Println("[WS] client registered")
		// Client unregistration, delete his connection
		case c := <-hub.unregister:
			hub.mu.Lock()
			if _, ok := hub.clients[c]; ok {
				delete(hub.clients, c)
				close(c.send)
			}
			hub.mu.Unlock()
			log.Println("[WS] client unregistered")
		// Broadcast message to all clients
		case msg := <-hub.broadcast:
			hub.mu.Lock()
			for c := range hub.clients {
				select {
				case c.send <- msg:
				default:
					close(c.send)
					delete(hub.clients, c)
				}
			}
			hub.mu.Unlock()
		}
	}
}

// readPump reads messages from the websocket connection.
func (c *Client) readPump() {
	defer func() {
		if c.playerID != "" {
			game.Default.RemovePlayer(c.playerID)
		}
		h.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Println("[WS] read error:", err)
			break
		}
		log.Println("[WS] recv:", string(message))
		// parse JSON message
		var m map[string]interface{}
		// decode json
		if err := json.Unmarshal(message, &m); err != nil {
			log.Println("[WS] invalid json:", err)
			continue
		}
		typeStr, _ := m["type"].(string)
		switch typeStr {
		case "join":
			name, _ := m["name"].(string)
			p := game.Default.AddPlayer(name)
			if p == nil {
				resp := map[string]interface{}{"type": "error", "message": "unable to add player"}
				b, _ := json.Marshal(resp)
				c.conn.WriteMessage(websocket.TextMessage, b)
				continue
			}
			c.playerID = p.ID
			ack := map[string]interface{}{"type": "join_ack", "id": p.ID, "pos": map[string]int{"x": p.X, "y": p.Y}, "grid": map[string]int{"w": game.Default.W, "h": game.Default.H}}
			b, _ := json.Marshal(ack)
			c.conn.WriteMessage(websocket.TextMessage, b)
		case "move":
			if c.playerID == "" {
				resp := map[string]interface{}{"type": "error", "message": "not joined"}
				b, _ := json.Marshal(resp)
				c.conn.WriteMessage(websocket.TextMessage, b)
				continue
			}
			dir, _ := m["dir"].(string)
			cmd := game.Command{PlayerID: c.playerID, Type: "move", Dir: dir}
			game.Default.PushCommand(cmd)
		default:
			// ignore unknown types for now
		}
	}
}

// writePump writes messages from the send channel to the websocket connection.
func (c *Client) writePump() {
	for msg := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			log.Println("[WS] write error:", err)
			break
		}
	}
	c.conn.Close()
}

// WS upgrades the HTTP connection to a WebSocket and registers the client.
func WS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("[WS] upgrade:", err)
		return
	}
	client := &Client{conn: conn, send: make(chan []byte, 256)}
	h.register <- client
	go client.writePump()
	client.readPump()
}
