package game

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// Player represents a player in the game.
type Player struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	Score int    `json:"score"`
}

// Sweet represents a collectible in the game.
type Sweet struct {
	ID string `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
}

// Command from player
type Command struct {
	PlayerID string
	Type     string // "move"
	Dir      string // "up","down","left","right"
	X        int
	Y        int
}

// StateMessage is what the server broadcasts each tick.
type StateMessage struct {
	Type    string    `json:"type"`
	Tick    int64     `json:"tick"`
	Players []*Player `json:"players"`
	Sweets  []*Sweet  `json:"sweets"`
}

// Game contains the game state and control channels.
type Game struct {
	W, H int
	mu   sync.Mutex
	// state
	players map[string]*Player
	sweets  map[string]*Sweet
	// control
	commands chan Command
	// broadcast state bytes
	StateBroadcast chan []byte
	// broadcast event bytes (e.g., collected)
	EventBroadcast chan []byte
	// tick counter
	tick int64
	// random
	rand *rand.Rand
}

// NewGame creates a new game and initializes sweets.
func NewGame(w, h, nSweets int) *Game {
	g := &Game{
		W:              w,
		H:              h,
		players:        make(map[string]*Player),
		sweets:         make(map[string]*Sweet),
		commands:       make(chan Command, 1024),
		StateBroadcast: make(chan []byte, 10),
		EventBroadcast: make(chan []byte, 10),
		rand:           rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for i := 0; i < nSweets; i++ {
		x := g.rand.Intn(w)
		y := g.rand.Intn(h)
		id := fmt.Sprintf("s%d", i+1)
		g.sweets[id] = &Sweet{ID: id, X: x, Y: y}
	}
	return g
}

// Start the game loop at ticksPerSec.
func (g *Game) Start(ticksPerSec int) {
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(ticksPerSec))
		defer ticker.Stop()
		for range ticker.C {
			g.tick++
			g.applyCommands()
			g.broadcastState()
		}
	}()
}

// applyCommands processes queued commands deterministically.
func (g *Game) applyCommands() {
	// collect commands
	cmds := make([]Command, 0)
	for {
		select {
		case c := <-g.commands:
			cmds = append(cmds, c)
		default:
			goto PROCESS
		}
	}
PROCESS:
	if len(cmds) == 0 {
		return
	}
	// group by player and keep last command per player while recording arrival index
	last := make(map[string]Command)
	lastIndex := make(map[string]int)
	for idx, c := range cmds {
		last[c.PlayerID] = c
		lastIndex[c.PlayerID] = idx
	}
	// create ordered list of players by arrival index of their last command
	type entry struct{ id string; idx int }
	entries := make([]entry, 0, len(lastIndex))
	for id, i := range lastIndex {
		entries = append(entries, entry{id: id, idx: i})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].idx < entries[j].idx })

	g.mu.Lock()
	defer g.mu.Unlock()
	// build occupancy map of current positions
	occupied := make(map[[2]int]string)
	for _, p := range g.players {
		occupied[[2]int{p.X, p.Y}] = p.ID
	}

	for _, e := range entries {
		id := e.id
		c := last[id]
		p, ok := g.players[id]
		if !ok {
			continue
		}
		// compute desired position
		nx, ny := p.X, p.Y
		switch c.Type {
		case "move":
			switch c.Dir {
			case "up":
				ny = max(0, p.Y-1)
			case "down":
				ny = min(g.H-1, p.Y+1)
			case "left":
				nx = max(0, p.X-1)
			case "right":
				nx = min(g.W-1, p.X+1)
			}
		case "move_abs":
			nx = clamp(c.X, 0, g.W-1)
			ny = clamp(c.Y, 0, g.H-1)
		}
		// if desired occupied by someone (who may move too), we only allow move if target not in occupied map or occupied by self
		if occ, exists := occupied[[2]int{nx, ny}]; exists && occ != p.ID {
			// conflict — skip move
			continue
		}
		// apply move
		delete(occupied, [2]int{p.X, p.Y})
		p.X, p.Y = nx, ny
		occupied[[2]int{p.X, p.Y}] = p.ID
		// check sweet
		for sid, s := range g.sweets {
			if s.X == p.X && s.Y == p.Y {
				p.Score++
				delete(g.sweets, sid)
				// emit collected event
				evt := map[string]interface{}{"type": "event", "event": "collected", "player": p.ID, "sweet": sid, "tick": g.tick}
				if b, err := json.Marshal(evt); err == nil {
					select {
					case g.EventBroadcast <- b:
					default:
						// drop if nobody consumes or backlog full
					}
				}
				break
			}
		}
	}
}

func (g *Game) broadcastState() {
	g.mu.Lock()
	players := make([]*Player, 0, len(g.players))
	for _, p := range g.players {
		players = append(players, &Player{ID: p.ID, Name: p.Name, X: p.X, Y: p.Y, Score: p.Score})
	}
	sweets := make([]*Sweet, 0, len(g.sweets))
	for _, s := range g.sweets {
		sweets = append(sweets, &Sweet{ID: s.ID, X: s.X, Y: s.Y})
	}
	g.mu.Unlock()

	msg := StateMessage{Type: "state", Tick: g.tick, Players: players, Sweets: sweets}
	b, _ := json.Marshal(msg)
	select {
	case g.StateBroadcast <- b:
	default:
		// drop if nobody consumes or backlog full
	}
}

// AddPlayer adds a player at a random free position and returns id and pointer to player.
func (g *Game) AddPlayer(name string) *Player {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := fmt.Sprintf("p-%d", len(g.players)+1)
	// find free spot
	for i := 0; i < 1000; i++ {
		x := g.rand.Intn(g.W)
		y := g.rand.Intn(g.H)
		free := true
		for _, p := range g.players {
			if p.X == x && p.Y == y {
				free = false
				break
			}
		}
		if free {
			p := &Player{ID: id, Name: name, X: x, Y: y, Score: 0}
			g.players[id] = p
			return p
		}
	}
	// fallback: find first free in grid
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			free := true
			for _, p := range g.players {
				if p.X == x && p.Y == y {
					free = false
					break
				}
			}
			if free {
				id := fmt.Sprintf("p-%d", len(g.players)+1)
				p := &Player{ID: id, Name: name, X: x, Y: y, Score: 0}
				g.players[id] = p
				return p
			}
		}
	}
	return nil
}

// Testing helpers (exported) -------------------------------------------------

// SetSweet places or replaces a sweet at the given coordinates (useful for tests).
func (g *Game) SetSweet(id string, x, y int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweets[id] = &Sweet{ID: id, X: x, Y: y}
}

// ClearSweets removes all sweets (useful for tests).
func (g *Game) ClearSweets() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweets = make(map[string]*Sweet)
}

// SetPlayerPosition moves a player instantly (for test setup).
func (g *Game) SetPlayerPosition(id string, x, y int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if p, ok := g.players[id]; ok {
		p.X = x
		p.Y = y
	}
}

// GetPlayer returns a copy of the player state (nil if not found).
func (g *Game) GetPlayer(id string) *Player {
	g.mu.Lock()
	defer g.mu.Unlock()
	if p, ok := g.players[id]; ok {
		cp := *p
		return &cp
	}
	return nil
}

// SweetsCount returns the number of sweets remaining.
func (g *Game) SweetsCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.sweets)
}

// RemovePlayer removes a player from the game state.
func (g *Game) RemovePlayer(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.players, id)
}

// PushCommand queues a command.
func (g *Game) PushCommand(c Command) {
	select {
	case g.commands <- c:
	default:
		// drop if full
	}
}

// helpers
func min(a, b int) int { if a < b { return a }; return b }
func max(a, b int) int { if a > b { return a }; return b }
func clamp(v, a, b int) int { if v < a { return a }; if v > b { return b }; return v }

// Default global game
var Default = NewGame(10, 10, 20)

func init() {
	Default.Start(10)
}
