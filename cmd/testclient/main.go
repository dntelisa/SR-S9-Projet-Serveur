package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	addr := flag.String("addr", "ws://localhost:8080/ws", "websocket address")
	name := flag.String("name", "bot", "player name")
	flag.Parse()

	c, _, err := websocket.DefaultDialer.Dial(*addr, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	// send join
	join := map[string]interface{}{"type": "join", "name": *name}
	b, _ := json.Marshal(join)
	c.WriteMessage(websocket.TextMessage, b)

	// read loop
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read error:", err)
				return
			}
			fmt.Println("RECV:", string(message))
		}
	}()

	// send moves periodically
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	for {
		select {
		case <-ticker.C:
			move := map[string]interface{}{"type": "move", "dir": "right"}
			bm, _ := json.Marshal(move)
			c.WriteMessage(websocket.TextMessage, bm)
	case <-sigs:
			fmt.Println("interrupt")
			return
	case <-done:
			return
		}
	}
}
