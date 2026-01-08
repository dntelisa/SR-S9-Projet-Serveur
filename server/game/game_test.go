package game

import (
	"encoding/json"
	"testing"
)

func TestAddPlayerAndBounds(t *testing.T) {
	g := NewGame(3, 3, 0)
	p := g.AddPlayer("A")
	if p == nil {
		t.Fatalf("expected player, got nil")
	}
	if p.X < 0 || p.X >= g.W || p.Y < 0 || p.Y >= g.H {
		t.Fatalf("player out of bounds: %v", p)
	}
}

func TestMoveAndCollision(t *testing.T) {
	g := NewGame(3, 3, 0)
	// place two players deterministically
	g.mu.Lock()
	g.players = map[string]*Player{"p-1": {ID: "p-1", Name: "A", X: 1, Y: 1, Score: 0}, "p-2": {ID: "p-2", Name: "B", X: 2, Y: 1, Score: 0}}
	g.mu.Unlock()
	// p-1 moves right into occupied cell (2,1) — should be blocked
	g.PushCommand(Command{PlayerID: "p-1", Type: "move", Dir: "right"})
	// p-2 stays
	g.applyCommands()
	if g.players["p-1"].X != 1 || g.players["p-1"].Y != 1 {
		t.Fatalf("expected p-1 to stay, got %d,%d", g.players["p-1"].X, g.players["p-1"].Y)
	}
}

func TestCollectSweet(t *testing.T) {
	g := NewGame(3, 3, 0)
	// player at (0,0), sweet at (1,0)
	g.mu.Lock()
	g.players = map[string]*Player{"p-1": {ID: "p-1", Name: "A", X: 0, Y: 0, Score: 0}}
	g.sweets = map[string]*Sweet{"s1": {ID: "s1", X: 1, Y: 0}}
	g.mu.Unlock()
	// move right to collect
	g.PushCommand(Command{PlayerID: "p-1", Type: "move", Dir: "right"})
	g.applyCommands()
	p := g.players["p-1"]
	if p.X != 1 || p.Y != 0 {
		t.Fatalf("expected player at 1,0 got %d,%d", p.X, p.Y)
	}
	if p.Score != 1 {
		t.Fatalf("expected score 1, got %d", p.Score)
	}
	if len(g.sweets) != 0 {
		t.Fatalf("expected no sweets left, got %d", len(g.sweets))
	}
}

func TestBroadcastStateFormat(t *testing.T) {
	g := NewGame(4, 4, 1)
	p := g.AddPlayer("X")
	// force a broadcast
	g.tick = 42
	g.broadcastState()
	select {
	case b := <-g.StateBroadcast:
		var msg StateMessage
		if err := json.Unmarshal(b, &msg); err != nil {
			t.Fatalf("invalid state json: %v", err)
		}
		if msg.Tick != 42 {
			t.Fatalf("tick mismatch: got %d", msg.Tick)
		}
		found := false
		for _, pl := range msg.Players {
			if pl.ID == p.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("player missing from state")
		}
	default:
		t.Fatalf("no state broadcast available")
	}
}

func TestConflictCollect(t *testing.T) {
	// Two players both attempt to move into the same cell containing a sweet
	g := NewGame(3, 3, 0)
	g.mu.Lock()
	g.players = map[string]*Player{
		"p-1": {ID: "p-1", Name: "A", X: 0, Y: 1, Score: 0},
		"p-2": {ID: "p-2", Name: "B", X: 2, Y: 1, Score: 0},
	}
	g.sweets = map[string]*Sweet{"s1": {ID: "s1", X: 1, Y: 1}}
	g.mu.Unlock()

	// Both move towards (1,1) in the same tick: p1 then p2 (p1 arrived first)
	g.PushCommand(Command{PlayerID: "p-1", Type: "move", Dir: "right"})
	g.PushCommand(Command{PlayerID: "p-2", Type: "move", Dir: "left"})
	g.applyCommands()

	p1 := g.players["p-1"]
	p2 := g.players["p-2"]
	// since p-1's command arrived first, p-1 should win and collect the sweet
	if p1.X != 1 || p1.Y != 1 {
		t.Fatalf("expected p-1 at 1,1 got %d,%d", p1.X, p1.Y)
	}
	if p1.Score != 1 {
		t.Fatalf("expected p-1 score 1 got %d", p1.Score)
	}
	// p-2 should have been blocked and not collected
	if p2.X != 2 || p2.Y != 1 {
		t.Fatalf("expected p-2 to stay at 2,1 got %d,%d", p2.X, p2.Y)
	}
	if p2.Score != 0 {
		t.Fatalf("expected p-2 score 0 got %d", p2.Score)
	}
	if len(g.sweets) != 0 {
		t.Fatalf("expected sweets empty, got %d", len(g.sweets))
	}
}

func TestConflictArrivalOrder(t *testing.T) {
	// same setup but push p2 then p1 to ensure arrival order gives p2 the sweet
	g := NewGame(3, 3, 0)
	g.mu.Lock()
	g.players = map[string]*Player{
		"p-1": {ID: "p-1", Name: "A", X: 0, Y: 1, Score: 0},
		"p-2": {ID: "p-2", Name: "B", X: 2, Y: 1, Score: 0},
	}
	g.sweets = map[string]*Sweet{"s1": {ID: "s1", X: 1, Y: 1}}
	g.mu.Unlock()

	// p2 command arrives first, then p1
	g.PushCommand(Command{PlayerID: "p-2", Type: "move", Dir: "left"})
	g.PushCommand(Command{PlayerID: "p-1", Type: "move", Dir: "right"})
	g.applyCommands()

	p1 := g.players["p-1"]
	p2 := g.players["p-2"]
	// p2 should win due to arrival order
	if p2.X != 1 || p2.Y != 1 {
		t.Fatalf("expected p-2 at 1,1 got %d,%d", p2.X, p2.Y)
	}
	if p2.Score != 1 {
		t.Fatalf("expected p-2 score 1 got %d", p2.Score)
	}
	// p1 should be blocked
	if p1.X != 0 || p1.Y != 1 {
		t.Fatalf("expected p-1 to stay at 0,1 got %d,%d", p1.X, p1.Y)
	}
	if p1.Score != 0 {
		t.Fatalf("expected p-1 score 0 got %d", p1.Score)
	}
	if len(g.sweets) != 0 {
		t.Fatalf("expected sweets empty, got %d", len(g.sweets))
	}
}