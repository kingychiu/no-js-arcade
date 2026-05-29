package arcade

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// SnakeRuntime owns the per-session Snake goroutines. It is the project's
// only piece of stateful in-memory machinery — every other game stores its
// state in SQLite and replays it on each request.
//
// The runtime is NOT thread-safe across processes (single-binary by design).
type SnakeRuntime struct {
	mu       sync.Mutex
	sessions map[string]*SnakeGoroutine
}

// NewSnakeRuntime returns an empty runtime ready to spawn sessions.
func NewSnakeRuntime() *SnakeRuntime {
	return &SnakeRuntime{
		sessions: make(map[string]*SnakeGoroutine),
	}
}

// SnakeGoroutine is one running game. Its public methods are safe to call
// from any goroutine.
type SnakeGoroutine struct {
	sessionID string
	tickRate  time.Duration
	onEnd     func(sessionID string, finalScore int)

	mu    sync.Mutex // protects board/state/score
	board SnakeBoard
	state SnakeState
	score int
	rng   *rand.Rand
	input chan SnakeDirection

	waitersMu sync.Mutex
	waiters   []chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
}

// Start launches a goroutine for the given session, replacing any existing
// goroutine for the same session. The onEnd callback fires once when the
// game ends (collision); use it to persist the leaderboard entry and
// transition the session's wizard state. Pass a seeded *rand.Rand for
// determinism in tests.
func (r *SnakeRuntime) Start(
	sessionID string,
	board SnakeBoard,
	tickRate time.Duration,
	rng *rand.Rand,
	onEnd func(sessionID string, finalScore int),
) *SnakeGoroutine {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.sessions[sessionID]; ok {
		existing.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	sg := &SnakeGoroutine{
		sessionID: sessionID,
		tickRate:  tickRate,
		onEnd:     onEnd,
		board:     board,
		state:     SnakePlaying,
		rng:       rng,
		input:     make(chan SnakeDirection, 4),
		ctx:       ctx,
		cancel:    cancel,
	}
	r.sessions[sessionID] = sg
	go sg.run()
	return sg
}

// Stop cancels the goroutine for the given session, if any. Safe to call
// even if no session exists.
func (r *SnakeRuntime) Stop(sessionID string) {
	r.mu.Lock()
	sg, ok := r.sessions[sessionID]
	if ok {
		delete(r.sessions, sessionID)
	}
	r.mu.Unlock()
	if ok {
		sg.cancel()
	}
}

// Get returns the running goroutine for the given session.
func (r *SnakeRuntime) Get(sessionID string) (*SnakeGoroutine, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sg, ok := r.sessions[sessionID]
	return sg, ok
}

// PushDirection forwards a direction-change to the goroutine's input channel.
// Drops silently if the channel buffer is full (player spamming keys).
func (sg *SnakeGoroutine) PushDirection(dir SnakeDirection) {
	select {
	case sg.input <- dir:
	default:
	}
}

// Snapshot returns a copy of the current board, FSM state, and score.
func (sg *SnakeGoroutine) Snapshot() (SnakeBoard, SnakeState, int) {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	return sg.board, sg.state, sg.score
}

// WaitNextFrame blocks until the goroutine produces another frame (or the
// caller's context is cancelled, or the goroutine itself stops). This is
// the long-poll primitive.
func (sg *SnakeGoroutine) WaitNextFrame(ctx context.Context) {
	ch := make(chan struct{}, 1)
	sg.waitersMu.Lock()
	sg.waiters = append(sg.waiters, ch)
	sg.waitersMu.Unlock()

	select {
	case <-ch:
	case <-ctx.Done():
	case <-sg.ctx.Done():
	}
}

func (sg *SnakeGoroutine) notifyAll() {
	sg.waitersMu.Lock()
	waiters := sg.waiters
	sg.waiters = nil
	sg.waitersMu.Unlock()
	for _, ch := range waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (sg *SnakeGoroutine) run() {
	ticker := time.NewTicker(sg.tickRate)
	defer ticker.Stop()

	for {
		select {
		case <-sg.ctx.Done():
			sg.notifyAll()
			return

		case dir := <-sg.input:
			sg.mu.Lock()
			sg.board = SetDirection(sg.board, dir)
			sg.mu.Unlock()

		case <-ticker.C:
			sg.mu.Lock()
			newBoard, newState := Tick(sg.board, sg.rng)
			sg.board = newBoard
			sg.state = newState
			sg.score = newBoard.Score
			sg.mu.Unlock()

			if newState == SnakeGameOver {
				if sg.onEnd != nil {
					sg.onEnd(sg.sessionID, newBoard.Score)
				}
				sg.notifyAll()
				return
			}
			sg.notifyAll()
		}
	}
}
