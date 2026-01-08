package game

import (
	"encoding/json"
	"testing"
)

func TestCollectedEventEmitted(t *testing.T) {
	g := NewGame(3, 3, 0)
	// place player and sweet deterministically
	p := g.AddPlayer("A")
	// move player to a safe position at (0,0)
	g.SetPlayerPosition(p.ID, 0, 0)
	g.SetSweet("s1", 1, 0)
	// ensure channel is drained if any
	select {
	default:
	}
	// push move to collect
	g.PushCommand(Command{PlayerID: p.ID, Type: "move", Dir: "right"})
	g.applyCommands()
	// expect event
	select {
	case b := <-g.EventBroadcast:
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("invalid event json: %v", err)
		}
		if m["type"] != "event" || m["event"] != "collected" {
			t.Fatalf("unexpected event: %v", m)
		}
		if m["player"] != p.ID {
			t.Fatalf("event player mismatch: %v", m)
		}
		if m["sweet"] != "s1" {
			t.Fatalf("event sweet mismatch: %v", m)
		}
	default:
		t.Fatalf("no collected event emitted")
	}
}