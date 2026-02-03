package game

import (
	"encoding/json"
	"fmt"
	"math/rand"
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
	W, H int // map size
	mu   sync.Mutex // protects below to avoid data races
	// state
	players map[string]*Player // key: player ID, value: pointer to Player
	sweets  map[string]*Sweet // key: sweet ID, value: pointer to Sweet
	// control
	commands chan Command // incoming commands from players in parallel
	// broadcast state bytes
	StateBroadcast chan []byte // chanel for broadcasting state, it's the output
	// broadcast event bytes (e.g., collected)
	EventBroadcast chan []byte // ponctual events like game over, sweet collected, player joined, etc.
	// tick counter
	tick int64 // if client receive packet in the wrong order, it will know how to handle it
	// random
	rand *rand.Rand // for random positions
}

// NewGame creates a new game and initializes sweets.
func NewGame(w, h, nSweets int) *Game {
	g := &Game{
		W:              w,
		H:              h,
		players:        make(map[string]*Player),
		sweets:         make(map[string]*Sweet),
		commands:       make(chan Command, 1024), // buffered channel for commands, to avoid blocking, it's like a big queue
		StateBroadcast: make(chan []byte, 10), // buffered channel for state broadcasts, like a small queue because state is frequent
		EventBroadcast: make(chan []byte, 10), // buffered channel for event broadcasts
		rand:           rand.New(rand.NewSource(time.Now().UnixNano())), // initialize random source
	}
	for i := 0; i < nSweets; i++ {
		x := g.rand.Intn(w) 
		y := g.rand.Intn(h)
		id := fmt.Sprintf("s%d", i+1) // give an unique id
		g.sweets[id] = &Sweet{ID: id, X: x, Y: y} // place sweet at random position
	}
	return g // return pointer to game, adress in memory of the game struct
}

// Start the game loop at ticksPerSec.
func (g *Game) Start(ticksPerSec int) {
	// goroutine for game loop, thread that runs concurrently
	// the main program listen http connexion (new players), without this goroutine the game state would not update
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(ticksPerSec)) // ticker to trigger ticks at regular intervals
		defer ticker.Stop() // clean up ticker when goroutine ends
		// main game loop, runs at each tick
		// Ensure that game runs at constant speed regardless of processing time
		for range ticker.C {
			g.tick++ // increment tick counter
			g.applyCommands() // process all queued commands (Input)
			g.broadcastState() // broadcast current state to all clients (Output)

			// Manage end of game, check at each tick if party is over
			if g.SweetsCount() == 0 {
				// Recover scores 
				g.mu.Lock() // Lock to read player scores safely (no problem if a player disconnects at this moment)
				players := make([]map[string]interface{}, 0, len(g.players)) // prepare scores slice
				for _, p := range g.players {
					players = append(players, map[string]interface{}{
						"id":    p.ID,
						"name":  p.Name,
						"score": p.Score,
					})
				}
				g.mu.Unlock()

				// Create message JSON for game over
				msg := map[string]interface{}{
					"type":   "game_over",
					"scores": players,
				}
				b, _ := json.Marshal(msg) // serialize to JSON

				// Broadcast game over message
				select {
				case g.EventBroadcast <- b:
				default: // drop if network is saturated or nobody is listening
				}

				// Restart game after a delay
				time.Sleep(5 * time.Second)
				g.Restart()
			}
		}
	}()
}

// applyCommands processes queued commands deterministically.
// Authorize or not the moves based on collisions and limits speed.
func (g *Game) applyCommands() {
	// collect commands
	cmds := make([]Command, 0)
	for {
		select {
		case c := <-g.commands: // while there are commands in the channel, store them in temporary cmds slice
			cmds = append(cmds, c) // list to treat all commands at once, it's for a tick
		default:
			goto PROCESS // exit loop when no more commands
		}
	}
PROCESS:
	if len(cmds) == 0 {
		return
	}

	// process commands, nobody else can modify game state during this
	g.mu.Lock()
	defer g.mu.Unlock() 

	// Limit speed: max 2 moves per tick
	movesCount := make(map[string]int)
	const MaxMovesPerTick = 2

	// Process commands in order
	for _, c := range cmds {
		// Ignore if exceeded move limit
		if movesCount[c.PlayerID] >= MaxMovesPerTick {
			continue
		}

		p, ok := g.players[c.PlayerID]
		if !ok {
			continue
		}

		// Compute new position
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
		}

		// Check for collisions with other players
		collision := false
		for _, other := range g.players {
			if other.ID != p.ID && other.X == nx && other.Y == ny {
				collision = true
				break
			}
		}

		// If no collision, apply move
		if !collision {
			p.X, p.Y = nx, ny
			movesCount[c.PlayerID]++

			// Check for sweet collection
			for sid, s := range g.sweets {
				if s.X == p.X && s.Y == p.Y {
					p.Score++
					delete(g.sweets, sid)
					// broadcast event
					evt := map[string]interface{}{"type": "event", "event": "collected", "player": p.ID, "sweet": sid, "tick": g.tick}
					if b, err := json.Marshal(evt); err == nil {
						select {
						case g.EventBroadcast <- b:
						default:
						}
					}
					break
				}
			}
		}
	}
}

func (g *Game) broadcastState() {
	// Block other modifications while reading state
	g.mu.Lock()
	players := make([]*Player, 0, len(g.players))
	for _, p := range g.players {
		// Create a copy of the player
		players = append(players, &Player{ID: p.ID, Name: p.Name, X: p.X, Y: p.Y, Score: p.Score})
	}
	sweets := make([]*Sweet, 0, len(g.sweets))
	for _, s := range g.sweets {
		// Create a copy of the sweet
		sweets = append(sweets, &Sweet{ID: s.ID, X: s.X, Y: s.Y})
	}
	// Unlock before marshaling to avoid holding lock too long
	g.mu.Unlock()

	msg := StateMessage{Type: "state", Tick: g.tick, Players: players, Sweets: sweets}
	b, _ := json.Marshal(msg)

	// Sending no blocking to avoid slowing down the game loop
	select {
	case g.StateBroadcast <- b:
	default:
		// drop if nobody consumes or backlog full
	}
}

// AddPlayer adds a player at a random free position and returns id and pointer to player.
func (g *Game) AddPlayer(name string) *Player {
	// Lock to avoid players appear at the same position
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
	// for the case of a nearly full grid and previous method does not find a spot
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
	// no space left
	return nil
}

// Restart resets the game state for a new round.
func (g *Game) Restart() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Reset Scores
	for _, p := range g.players {
		p.Score = 0
	}

	// Regen Sweets (20 sweets)
	g.sweets = make(map[string]*Sweet)
	for i := 0; i < 20; i++ {
		x := g.rand.Intn(g.W)
		y := g.rand.Intn(g.H)
		id := fmt.Sprintf("s%d", i+1)
		g.sweets[id] = &Sweet{ID: id, X: x, Y: y}
	}

	// Clear pending commands
LOOP:
	for {
		select {
		case <-g.commands:
		default:
			break LOOP
		}
	}
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
	Default.Start(20)
}
