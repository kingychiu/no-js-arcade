package arcade

import (
	"math/rand"
)

// MSState is the lifecycle phase of a Minesweeper game.
type MSState string

const (
	MSPlaying MSState = "playing"
	MSWon     MSState = "won"
	MSLost    MSState = "lost"
)

// CanTransitionTo enforces the legal phases.
func (s MSState) CanTransitionTo(next MSState) bool {
	if s == MSPlaying {
		return next == MSWon || next == MSLost
	}
	return false
}

// MSCell is one square on the board.
type MSCell struct {
	HasMine   bool `json:"m,omitempty"`
	Revealed  bool `json:"r,omitempty"`
	Flagged   bool `json:"f,omitempty"`
	Neighbors int  `json:"n,omitempty"`
}

// MSBoard is the in-memory Minesweeper game state.
type MSBoard struct {
	Width       int        `json:"w"`
	Height      int        `json:"h"`
	Cells       [][]MSCell `json:"c"`
	MineCount   int        `json:"mc"`
	MinesPlaced bool       `json:"mp"`
	Revealed    int        `json:"r"`           // count of revealed non-mine cells
	LostAt      [2]int     `json:"l,omitempty"` // -1,-1 if not lost
}

// MSDimensions returns (width, height, mineCount) for the given difficulty.
//
//	Easy   → 9×9   / 10 mines
//	Medium → 16×16 / 40 mines
//	Hard   → 24×24 / 99 mines
func MSDimensions(d Difficulty) (int, int, int) {
	switch d {
	case DiffEasy:
		return 9, 9, 10
	case DiffHard:
		return 24, 24, 99
	}
	return 16, 16, 40
}

// NewMSBoard returns an empty board. Mines are placed only on the first
// RevealCell call so the first click is guaranteed safe.
func NewMSBoard(width, height, mineCount int) MSBoard {
	cells := make([][]MSCell, height)
	for y := range cells {
		cells[y] = make([]MSCell, width)
	}
	return MSBoard{
		Width:     width,
		Height:    height,
		Cells:     cells,
		MineCount: mineCount,
		LostAt:    [2]int{-1, -1},
	}
}

// RevealCell reveals the cell at (x, y) and returns the new board and state.
// On the very first reveal of the game, mines are placed avoiding (x, y) and
// its 8 neighbors so the opening click is always safe. If the cell is empty
// (0 adjacent mines), neighbors are flood-revealed as in classic Minesweeper.
func RevealCell(board MSBoard, x, y int, rng *rand.Rand) (MSBoard, MSState) {
	if !inBounds(board, x, y) {
		return board, classifyMSState(board)
	}
	cell := board.Cells[y][x]
	if cell.Revealed || cell.Flagged {
		return board, classifyMSState(board)
	}

	if !board.MinesPlaced {
		board = placeMines(board, x, y, rng)
		board.MinesPlaced = true
	}

	if board.Cells[y][x].HasMine {
		board.Cells[y][x].Revealed = true
		board.LostAt = [2]int{x, y}
		// Reveal all mines on loss so the player sees the layout.
		for j := 0; j < board.Height; j++ {
			for i := 0; i < board.Width; i++ {
				if board.Cells[j][i].HasMine {
					board.Cells[j][i].Revealed = true
				}
			}
		}
		return board, MSLost
	}

	// Flood-fill reveal starting at (x, y).
	queue := [][2]int{{x, y}}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		cx, cy := p[0], p[1]
		if !inBounds(board, cx, cy) {
			continue
		}
		c := &board.Cells[cy][cx]
		if c.Revealed || c.Flagged || c.HasMine {
			continue
		}
		c.Revealed = true
		board.Revealed++
		if c.Neighbors == 0 {
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					queue = append(queue, [2]int{cx + dx, cy + dy})
				}
			}
		}
	}

	return board, classifyMSState(board)
}

// FlagCell toggles a flag on the cell at (x, y). Revealed cells cannot be
// flagged. The FSM state never changes from a flag.
func FlagCell(board MSBoard, x, y int) (MSBoard, MSState) {
	if !inBounds(board, x, y) {
		return board, classifyMSState(board)
	}
	cell := &board.Cells[y][x]
	if cell.Revealed {
		return board, classifyMSState(board)
	}
	cell.Flagged = !cell.Flagged
	return board, classifyMSState(board)
}

// MSScore is the leaderboard score: number of non-mine cells revealed. Higher
// is better; winning the game yields width*height - mineCount.
func MSScore(board MSBoard) int {
	return board.Revealed
}

// classifyMSState returns the lifecycle phase implied by the board.
func classifyMSState(board MSBoard) MSState {
	if board.LostAt[0] >= 0 {
		return MSLost
	}
	safeTotal := board.Width*board.Height - board.MineCount
	if board.Revealed >= safeTotal {
		return MSWon
	}
	return MSPlaying
}

// placeMines places mineCount mines on the board, avoiding (safeX, safeY) and
// its 8 neighbors, then computes the Neighbors count for every cell.
func placeMines(board MSBoard, safeX, safeY int, rng *rand.Rand) MSBoard {
	safe := map[[2]int]bool{}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			safe[[2]int{safeX + dx, safeY + dy}] = true
		}
	}

	candidates := make([][2]int, 0, board.Width*board.Height)
	for y := 0; y < board.Height; y++ {
		for x := 0; x < board.Width; x++ {
			if !safe[[2]int{x, y}] {
				candidates = append(candidates, [2]int{x, y})
			}
		}
	}

	mines := board.MineCount
	if mines > len(candidates) {
		mines = len(candidates)
	}
	rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	for i := 0; i < mines; i++ {
		p := candidates[i]
		board.Cells[p[1]][p[0]].HasMine = true
	}

	// Compute neighbor counts for non-mine cells.
	for y := 0; y < board.Height; y++ {
		for x := 0; x < board.Width; x++ {
			if board.Cells[y][x].HasMine {
				continue
			}
			board.Cells[y][x].Neighbors = countAdjacentMines(board, x, y)
		}
	}

	return board
}

func countAdjacentMines(board MSBoard, x, y int) int {
	count := 0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if !inBounds(board, nx, ny) {
				continue
			}
			if board.Cells[ny][nx].HasMine {
				count++
			}
		}
	}
	return count
}

func inBounds(board MSBoard, x, y int) bool {
	return x >= 0 && y >= 0 && x < board.Width && y < board.Height
}
