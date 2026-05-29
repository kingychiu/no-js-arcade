package arcade

import (
	"math/rand"
	"time"
)

// SnakeState is the lifecycle phase of a Snake game.
type SnakeState string

const (
	SnakeIdle     SnakeState = "idle"
	SnakePlaying  SnakeState = "playing"
	SnakeGameOver SnakeState = "game_over"
)

// CanTransitionTo enforces the legal phases.
func (s SnakeState) CanTransitionTo(next SnakeState) bool {
	switch s {
	case SnakeIdle:
		return next == SnakePlaying
	case SnakePlaying:
		return next == SnakeGameOver
	}
	return false
}

// SnakeDirection encodes a cardinal direction.
type SnakeDirection string

const (
	SnakeNorth SnakeDirection = "N"
	SnakeSouth SnakeDirection = "S"
	SnakeEast  SnakeDirection = "E"
	SnakeWest  SnakeDirection = "W"
)

// ValidSnakeDirection reports whether the value is recognized.
func ValidSnakeDirection(d SnakeDirection) bool {
	switch d {
	case SnakeNorth, SnakeSouth, SnakeEast, SnakeWest:
		return true
	}
	return false
}

// SnakeCell is one (x, y) position on the grid.
type SnakeCell struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// SnakeBoard is the in-memory game state.
type SnakeBoard struct {
	Width     int            `json:"w"`
	Height    int            `json:"h"`
	Snake     []SnakeCell    `json:"s"` // head at index 0
	Direction SnakeDirection `json:"d"`
	Food      SnakeCell      `json:"f"`
	Score     int            `json:"sc"`
}

// SnakeDimensions returns the grid (width, height) and tick interval for a
// difficulty. Grid is fixed at 20×15; difficulty controls the tick rate.
func SnakeDimensions(d Difficulty) (int, int, time.Duration) {
	switch d {
	case DiffEasy:
		return 20, 15, 250 * time.Millisecond
	case DiffHard:
		return 20, 15, 80 * time.Millisecond
	}
	return 20, 15, 150 * time.Millisecond
}

// NewSnakeBoard returns a fresh board with the snake centered horizontally,
// 3 segments long, heading east, and one food placed randomly.
func NewSnakeBoard(width, height int, rng *rand.Rand) SnakeBoard {
	cx, cy := width/2, height/2
	snake := []SnakeCell{
		{cx, cy},
		{cx - 1, cy},
		{cx - 2, cy},
	}
	return SnakeBoard{
		Width:     width,
		Height:    height,
		Snake:     snake,
		Direction: SnakeEast,
		Food:      spawnFood(width, height, snake, rng),
		Score:     0,
	}
}

// Tick advances the snake one step. Pure function: takes the board and an
// RNG (used to place new food when the snake eats), returns the new board
// and the implied FSM state (Playing or GameOver).
func Tick(board SnakeBoard, rng *rand.Rand) (SnakeBoard, SnakeState) {
	if len(board.Snake) == 0 {
		return board, SnakeGameOver
	}
	head := board.Snake[0]
	var next SnakeCell
	switch board.Direction {
	case SnakeNorth:
		next = SnakeCell{head.X, head.Y - 1}
	case SnakeSouth:
		next = SnakeCell{head.X, head.Y + 1}
	case SnakeEast:
		next = SnakeCell{head.X + 1, head.Y}
	case SnakeWest:
		next = SnakeCell{head.X - 1, head.Y}
	default:
		return board, SnakeGameOver
	}

	// Wall collision.
	if next.X < 0 || next.Y < 0 || next.X >= board.Width || next.Y >= board.Height {
		return board, SnakeGameOver
	}
	// Self collision (skip the tail because it'll move out of the way unless we eat).
	checkLen := len(board.Snake)
	for i := 0; i < checkLen-1; i++ {
		if board.Snake[i] == next {
			return board, SnakeGameOver
		}
	}

	newSnake := make([]SnakeCell, 0, checkLen+1)
	newSnake = append(newSnake, next)
	newSnake = append(newSnake, board.Snake...)

	if next == board.Food {
		// Ate food: keep tail, increment score, spawn new food.
		board.Score++
		board.Food = spawnFood(board.Width, board.Height, newSnake, rng)
	} else {
		// Move: trim tail.
		newSnake = newSnake[:len(newSnake)-1]
	}
	board.Snake = newSnake
	return board, SnakePlaying
}

// SetDirection updates the desired direction. Reverse-into-self is rejected;
// the input board is returned unchanged.
func SetDirection(board SnakeBoard, dir SnakeDirection) SnakeBoard {
	if isOpposite(board.Direction, dir) {
		return board
	}
	board.Direction = dir
	return board
}

func isOpposite(a, b SnakeDirection) bool {
	switch a {
	case SnakeNorth:
		return b == SnakeSouth
	case SnakeSouth:
		return b == SnakeNorth
	case SnakeEast:
		return b == SnakeWest
	case SnakeWest:
		return b == SnakeEast
	}
	return false
}

// spawnFood picks a random empty cell. Returns {-1,-1} if the board is full.
func spawnFood(width, height int, snake []SnakeCell, rng *rand.Rand) SnakeCell {
	occupied := make(map[SnakeCell]bool, len(snake))
	for _, c := range snake {
		occupied[c] = true
	}
	empties := make([]SnakeCell, 0, width*height-len(snake))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := SnakeCell{x, y}
			if !occupied[c] {
				empties = append(empties, c)
			}
		}
	}
	if len(empties) == 0 {
		return SnakeCell{-1, -1}
	}
	return empties[rng.Intn(len(empties))]
}

// SnakeBoardView is the template-friendly form of a board: a 2D grid of cell
// labels ("head", "body", "food", "empty") plus score and dimensions.
type SnakeBoardView struct {
	Score  int
	Width  int
	Height int
	Cells  [][]string
}

// NewSnakeBoardView projects a SnakeBoard into a renderable view.
func NewSnakeBoardView(board SnakeBoard) SnakeBoardView {
	cells := make([][]string, board.Height)
	for y := range cells {
		cells[y] = make([]string, board.Width)
		for x := range cells[y] {
			cells[y][x] = "empty"
		}
	}
	for i, c := range board.Snake {
		if c.X >= 0 && c.X < board.Width && c.Y >= 0 && c.Y < board.Height {
			if i == 0 {
				cells[c.Y][c.X] = "head"
			} else {
				cells[c.Y][c.X] = "body"
			}
		}
	}
	if board.Food.X >= 0 && board.Food.X < board.Width && board.Food.Y >= 0 && board.Food.Y < board.Height {
		cells[board.Food.Y][board.Food.X] = "food"
	}
	return SnakeBoardView{
		Score:  board.Score,
		Width:  board.Width,
		Height: board.Height,
		Cells:  cells,
	}
}
