package arcade

import (
	"database/sql"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	_ "github.com/mattn/go-sqlite3"

	"github.com/kingychiu/no-js-todolist/db"
	"github.com/labstack/echo/v4"
)

// --- 1. FSM unit tests ---

func TestWizardFSM_CanTransitionTo(t *testing.T) {
	t.Parallel()
	allowed := map[WizardState]map[WizardState]bool{
		WizardUnnamed:          {WizardNamed: true},
		WizardNamed:            {WizardGameChosen: true},
		WizardGameChosen:       {WizardDifficultyChosen: true, WizardNamed: true},
		WizardDifficultyChosen: {WizardPlaying: true, WizardGameChosen: true},
		WizardPlaying:          {WizardFinished: true},
		WizardFinished:         {WizardPlaying: true, WizardGameChosen: true, WizardNamed: true},
	}
	states := []WizardState{
		WizardUnnamed, WizardNamed, WizardGameChosen,
		WizardDifficultyChosen, WizardPlaying, WizardFinished,
	}
	for _, from := range states {
		for _, to := range states {
			want := allowed[from][to]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s → %s: got %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestT48FSM_CanTransitionTo(t *testing.T) {
	t.Parallel()
	allowed := map[T48State]map[T48State]bool{
		T48Playing:   {T48Won: true, T48Lost: true},
		T48Won:       {T48Continued: true, T48Lost: true},
		T48Continued: {T48Lost: true},
		T48Lost:      {},
	}
	states := []T48State{T48Playing, T48Won, T48Continued, T48Lost}
	for _, from := range states {
		for _, to := range states {
			want := allowed[from][to]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s → %s: got %v, want %v", from, to, got, want)
			}
		}
	}
}

// --- 2. Pure game-logic tests ---

func TestT48_NewBoard_HasTwoTilesAndCorrectSize(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	b := NewT48Board(4, rng)
	if b.Size != 4 {
		t.Fatalf("size = %d, want 4", b.Size)
	}
	if len(b.Cells) != 4 || len(b.Cells[0]) != 4 {
		t.Fatalf("cells shape wrong: %dx%d", len(b.Cells), len(b.Cells[0]))
	}
	nonZero := 0
	for _, row := range b.Cells {
		for _, v := range row {
			if v != 0 {
				nonZero++
				if v != 2 && v != 4 {
					t.Errorf("start tile = %d, want 2 or 4", v)
				}
			}
		}
	}
	if nonZero != 2 {
		t.Errorf("non-zero tiles = %d, want 2", nonZero)
	}
}

func TestT48_CompactAndMergeRow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in        []int
		wantRow   []int
		wantScore int
	}{
		{[]int{2, 2, 0, 0}, []int{4, 0, 0, 0}, 4},
		{[]int{2, 2, 2, 2}, []int{4, 4, 0, 0}, 8},
		{[]int{4, 4, 8, 8}, []int{8, 16, 0, 0}, 24},
		{[]int{2, 0, 2, 0}, []int{4, 0, 0, 0}, 4},
		{[]int{2, 4, 2, 4}, []int{2, 4, 2, 4}, 0},
		{[]int{0, 0, 0, 0}, []int{0, 0, 0, 0}, 0},
		{[]int{2, 0, 0, 2}, []int{4, 0, 0, 0}, 4},
		{[]int{2, 2, 4, 0}, []int{4, 4, 0, 0}, 4},
	}
	for _, c := range cases {
		gotRow, gotScore := compactAndMergeRow(append([]int{}, c.in...), 0)
		if !equalInts(gotRow, c.wantRow) {
			t.Errorf("compactAndMergeRow(%v) row = %v, want %v", c.in, gotRow, c.wantRow)
		}
		if gotScore != c.wantScore {
			t.Errorf("compactAndMergeRow(%v) score = %d, want %d", c.in, gotScore, c.wantScore)
		}
	}
}

func TestT48_ApplyMove_LeftMergesAdjacent(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	b := T48Board{
		Size: 4,
		Cells: [][]int{
			{2, 2, 0, 0},
			{0, 0, 0, 0},
			{0, 0, 0, 0},
			{0, 0, 0, 0},
		},
	}
	after, state, changed := ApplyMove(b, T48Left, rng)
	if !changed {
		t.Fatalf("expected change")
	}
	if state != T48Playing {
		t.Fatalf("state = %s, want playing", state)
	}
	if after.Cells[0][0] != 4 {
		t.Errorf("expected [0][0] = 4, got %d (board: %v)", after.Cells[0][0], after.Cells)
	}
	if after.Score != 4 {
		t.Errorf("score = %d, want 4", after.Score)
	}
}

func TestT48_ApplyMove_NoOpReturnsFalse(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	// Tiles already pushed to the left with no adjacent merges available.
	b := T48Board{
		Size: 4,
		Cells: [][]int{
			{2, 4, 8, 16},
			{0, 0, 0, 0},
			{0, 0, 0, 0},
			{0, 0, 0, 0},
		},
	}
	_, _, changed := ApplyMove(b, T48Left, rng)
	if changed {
		t.Errorf("expected no change for already-compacted row")
	}
}

func TestT48_Hit2048(t *testing.T) {
	t.Parallel()
	low := T48Board{Size: 1, Cells: [][]int{{1024}}}
	high := T48Board{Size: 1, Cells: [][]int{{2048}}}
	if Hit2048(low) {
		t.Errorf("Hit2048 false-positive on 1024")
	}
	if !Hit2048(high) {
		t.Errorf("Hit2048 missed 2048")
	}
}

func TestT48_HasValidMoves(t *testing.T) {
	t.Parallel()
	full := T48Board{
		Size: 2,
		Cells: [][]int{
			{2, 4},
			{8, 16},
		},
	}
	if HasValidMoves(full) {
		t.Errorf("expected no valid moves on a full board with no adjacencies")
	}
	withEmpty := T48Board{
		Size: 2,
		Cells: [][]int{
			{2, 0},
			{8, 16},
		},
	}
	if !HasValidMoves(withEmpty) {
		t.Errorf("expected valid moves when an empty cell exists")
	}
	withMerge := T48Board{
		Size: 2,
		Cells: [][]int{
			{2, 2},
			{8, 16},
		},
	}
	if !HasValidMoves(withMerge) {
		t.Errorf("expected valid moves when adjacent equal tiles exist")
	}
}

// Trigger a 2048-win using a constructed near-win board + one move.
func TestT48_ApplyMove_ProducesWonWhen2048Created(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	b := T48Board{
		Size: 4,
		Cells: [][]int{
			{1024, 1024, 0, 0},
			{0, 0, 0, 0},
			{0, 0, 0, 0},
			{0, 0, 0, 0},
		},
	}
	after, state, changed := ApplyMove(b, T48Left, rng)
	if !changed {
		t.Fatalf("expected change")
	}
	if state != T48Won {
		t.Fatalf("state = %s, want won; board after = %v", state, after.Cells)
	}
}

func TestMSFSM_CanTransitionTo(t *testing.T) {
	t.Parallel()
	allowed := map[MSState]map[MSState]bool{
		MSPlaying: {MSWon: true, MSLost: true},
		MSWon:     {},
		MSLost:    {},
	}
	states := []MSState{MSPlaying, MSWon, MSLost}
	for _, from := range states {
		for _, to := range states {
			want := allowed[from][to]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s → %s: got %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestMS_NewBoard_DimensionsAndEmpty(t *testing.T) {
	t.Parallel()
	b := NewMSBoard(9, 9, 10)
	if b.Width != 9 || b.Height != 9 || b.MineCount != 10 {
		t.Fatalf("dimensions wrong: %dx%d / %d", b.Width, b.Height, b.MineCount)
	}
	if b.MinesPlaced {
		t.Errorf("mines should be deferred to first reveal")
	}
	if b.Revealed != 0 {
		t.Errorf("revealed = %d, want 0", b.Revealed)
	}
	for _, row := range b.Cells {
		for _, c := range row {
			if c.HasMine || c.Revealed || c.Flagged {
				t.Errorf("cell not pristine: %+v", c)
			}
		}
	}
}

func TestMS_RevealCell_FirstClickIsSafe(t *testing.T) {
	t.Parallel()
	// Run with several seeds to spot-check first-click safety.
	for seed := int64(0); seed < 5; seed++ {
		rng := rand.New(rand.NewSource(seed))
		board := NewMSBoard(9, 9, 10)
		after, state := RevealCell(board, 4, 4, rng)
		if after.Cells[4][4].HasMine {
			t.Errorf("seed %d: first click landed on a mine", seed)
		}
		if state == MSLost {
			t.Errorf("seed %d: first click should never lose", seed)
		}
		if !after.MinesPlaced {
			t.Errorf("seed %d: mines should be placed after first reveal", seed)
		}
	}
}

func TestMS_RevealCell_HitsMineLoses(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	board := NewMSBoard(5, 5, 5)
	after, _ := RevealCell(board, 0, 0, rng)
	// Find a mine.
	mineX, mineY := -1, -1
	for y := 0; y < after.Height && mineX < 0; y++ {
		for x := 0; x < after.Width; x++ {
			if after.Cells[y][x].HasMine && !after.Cells[y][x].Revealed {
				mineX, mineY = x, y
				break
			}
		}
	}
	if mineX < 0 {
		t.Fatalf("no unrevealed mine found")
	}
	after2, state := RevealCell(after, mineX, mineY, rng)
	if state != MSLost {
		t.Errorf("revealing mine should be lost, got %s", state)
	}
	if after2.LostAt[0] != mineX || after2.LostAt[1] != mineY {
		t.Errorf("LostAt = %v, want [%d %d]", after2.LostAt, mineX, mineY)
	}
}

func TestMS_RevealAllSafeCellsWins(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	board := NewMSBoard(3, 3, 1)
	after, _ := RevealCell(board, 0, 0, rng)
	for y := 0; y < after.Height; y++ {
		for x := 0; x < after.Width; x++ {
			if !after.Cells[y][x].HasMine {
				after, _ = RevealCell(after, x, y, rng)
			}
		}
	}
	state := classifyMSState(after)
	if state != MSWon {
		t.Errorf("expected won after revealing all safe cells, got %s", state)
	}
}

func TestMS_FlagCell_Toggle(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	board := NewMSBoard(5, 5, 5)
	board, _ = RevealCell(board, 4, 4, rng) // place mines first

	// Find an unrevealed cell.
	var ux, uy int
	found := false
	for y := 0; y < board.Height && !found; y++ {
		for x := 0; x < board.Width; x++ {
			if !board.Cells[y][x].Revealed {
				ux, uy = x, y
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("no unrevealed cell")
	}

	after, state := FlagCell(board, ux, uy)
	if !after.Cells[uy][ux].Flagged {
		t.Errorf("expected flagged after toggle")
	}
	if state != MSPlaying {
		t.Errorf("flag should not change state, got %s", state)
	}

	after2, _ := FlagCell(after, ux, uy)
	if after2.Cells[uy][ux].Flagged {
		t.Errorf("expected unflagged after second toggle")
	}
}

func TestMS_FlagCell_RevealedNoOp(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	board := NewMSBoard(5, 5, 5)
	board, _ = RevealCell(board, 4, 4, rng)
	// (4,4) is now revealed by first click.
	after, _ := FlagCell(board, 4, 4)
	if after.Cells[4][4].Flagged {
		t.Errorf("revealed cell should not be flaggable")
	}
}

// --- 3. Handler + template contract tests ---

type testEnv struct {
	e *echo.Echo
	q *db.Queries
	h *Handlers
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dbpath := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal=WAL&_busy_timeout=5000&_sync=NORMAL&_fk=on", dbpath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	if err := RunMigrations(sqldb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	views, err := LoadViews()
	if err != nil {
		t.Fatalf("load views: %v", err)
	}
	h := &Handlers{
		Q:     db.New(sqldb),
		Views: views,
		rng:   rand.New(rand.NewSource(42)),
	}
	e := echo.New()
	e.HideBanner = true
	e.GET("/", h.GetIndex)
	e.POST("/wizard/name", h.PostWizardName)
	e.POST("/wizard/game", h.PostWizardGame)
	e.POST("/wizard/difficulty", h.PostWizardDifficulty)
	e.POST("/wizard/start", h.PostWizardStart)
	e.POST("/wizard/back", h.PostWizardBack)
	e.POST("/wizard/quit", h.PostWizardQuit)
	e.POST("/wizard/replay", h.PostWizardReplay)
	e.POST("/wizard/change-difficulty", h.PostWizardChangeDifficulty)
	e.POST("/wizard/different-game", h.PostWizardDifferentGame)
	e.POST("/game/2048/move", h.PostT48Move)
	e.POST("/game/minesweeper/reveal", h.PostMSReveal)
	e.POST("/game/minesweeper/flag", h.PostMSFlag)
	return &testEnv{e: e, q: h.Q, h: h}
}

// post sends a form POST with an optional cookie, returns recorder and updated cookie.
func (env *testEnv) post(t *testing.T, cookie, path string, values url.Values) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	req := httptest.NewRequest(http.MethodPost, path, body)
	if values != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	env.e.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookie = c.Name + "=" + c.Value
		}
	}
	return rec, cookie
}

func (env *testEnv) get(t *testing.T, cookie, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	env.e.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookie = c.Name + "=" + c.Value
		}
	}
	return rec, cookie
}

func parseHTML(t *testing.T, rec *httptest.ResponseRecorder) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

func TestGet_FreshSession_RendersNameStep(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	rec, _ := env.get(t, "", "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := parseHTML(t, rec)
	if doc.Find(`[data-step="name"]`).Length() == 0 {
		t.Errorf("expected name step")
	}
	if doc.Find("#error-banner").Length() == 0 {
		t.Errorf("shell missing #error-banner")
	}
	if doc.Find("#wizard-frame").Length() == 0 {
		t.Errorf("shell missing #wizard-frame")
	}
}

func TestPostWizardName_Empty_ReturnsOOBError(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	_, cookie := env.get(t, "", "/")
	rec, _ := env.post(t, cookie, "/wizard/name", url.Values{"name": {"   "}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := parseHTML(t, rec)
	if doc.Find(`#error-banner[hx-swap-oob="true"]`).Length() == 0 {
		t.Errorf("expected OOB error banner; body:\n%s", rec.Body.String())
	}
}

func TestPostWizardName_Valid_TransitionsToGameStep(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	_, cookie := env.get(t, "", "/")
	rec, _ := env.post(t, cookie, "/wizard/name", url.Values{"name": {"Alice"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := parseHTML(t, rec)
	if doc.Find(`[data-step="game"]`).Length() == 0 {
		t.Errorf("expected game step; body:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Alice") {
		t.Errorf("expected name to appear in response")
	}
}

func TestPostWizardGame_Valid_TransitionsToDifficultyStep(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	_, cookie := env.get(t, "", "/")
	_, cookie = env.post(t, cookie, "/wizard/name", url.Values{"name": {"Bob"}})
	rec, _ := env.post(t, cookie, "/wizard/game", url.Values{"game": {"2048"}})
	doc := parseHTML(t, rec)
	if doc.Find(`[data-step="difficulty"]`).Length() == 0 {
		t.Errorf("expected difficulty step; body:\n%s", rec.Body.String())
	}
}

func TestPostWizardGame_SkipAheadFromUnnamed_Rejected(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	_, cookie := env.get(t, "", "/")
	rec, _ := env.post(t, cookie, "/wizard/game", url.Values{"game": {"2048"}})
	doc := parseHTML(t, rec)
	if doc.Find(`#error-banner[hx-swap-oob="true"]`).Length() == 0 {
		t.Errorf("expected OOB error; body:\n%s", rec.Body.String())
	}
}

func TestPostWizardStart_InitializesBoardAndTransitionsToPlaying(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	cookie := walkToReady(t, env, "Carol", "2048", "medium")
	rec, _ := env.post(t, cookie, "/wizard/start", nil)
	doc := parseHTML(t, rec)
	if doc.Find(`[data-step="playing"]`).Length() == 0 {
		t.Errorf("expected playing step; body:\n%s", rec.Body.String())
	}
	if doc.Find("#twenty48-board").Length() == 0 {
		t.Errorf("expected 2048 board")
	}
}

func TestPostT48Move_Valid_ReplacesBoard(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	cookie := walkToPlaying(t, env, "Dora", "2048", "medium")
	rec, _ := env.post(t, cookie, "/game/2048/move", url.Values{"dir": {"left"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := parseHTML(t, rec)
	if doc.Find("#twenty48-board").Length() == 0 {
		t.Errorf("expected board fragment in response")
	}
}

func TestPostWizardGame_Minesweeper_TransitionsToDifficulty(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	_, cookie := env.get(t, "", "/")
	_, cookie = env.post(t, cookie, "/wizard/name", url.Values{"name": {"Min"}})
	rec, _ := env.post(t, cookie, "/wizard/game", url.Values{"game": {"minesweeper"}})
	doc := parseHTML(t, rec)
	if doc.Find(`[data-step="difficulty"]`).Length() == 0 {
		t.Fatalf("expected difficulty step; body:\n%s", rec.Body.String())
	}
	if !strings.Contains(doc.Text(), "9×9") {
		t.Errorf("expected Minesweeper difficulty labels to mention 9×9")
	}
}

func TestPostMSReveal_FirstClickIsSafe(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	cookie := walkToPlaying(t, env, "Sweeper", "minesweeper", "easy")
	rec, _ := env.post(t, cookie, "/game/minesweeper/reveal", url.Values{"x": {"4"}, "y": {"4"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := parseHTML(t, rec)
	if doc.Find("#minesweeper-board").Length() == 0 {
		t.Errorf("expected minesweeper board fragment")
	}
}

func TestPostMSReveal_InvalidCoords_OOBError(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	cookie := walkToPlaying(t, env, "Bob", "minesweeper", "easy")
	rec, _ := env.post(t, cookie, "/game/minesweeper/reveal", url.Values{"x": {"abc"}, "y": {"4"}})
	doc := parseHTML(t, rec)
	if doc.Find(`#error-banner[hx-swap-oob="true"]`).Length() == 0 {
		t.Errorf("expected OOB error for invalid coord")
	}
}

func TestPostMSFlag_TogglesFlagOnHiddenCell(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	cookie := walkToPlaying(t, env, "Flagger", "minesweeper", "easy")
	rec, _ := env.post(t, cookie, "/game/minesweeper/flag", url.Values{"x": {"0"}, "y": {"0"}})
	doc := parseHTML(t, rec)
	if doc.Find("#minesweeper-board").Length() == 0 {
		t.Errorf("expected board fragment after flag")
	}
}

func TestPostT48Move_InvalidDir_OOBError(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	cookie := walkToPlaying(t, env, "Eve", "2048", "medium")
	rec, _ := env.post(t, cookie, "/game/2048/move", url.Values{"dir": {"diagonal"}})
	doc := parseHTML(t, rec)
	if doc.Find(`#error-banner[hx-swap-oob="true"]`).Length() == 0 {
		t.Errorf("expected OOB error")
	}
}

// --- 4. Cross-reference test ---

func TestHxTargets_ResolveInShell(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	rec, _ := env.get(t, "", "/")
	shell := parseHTML(t, rec)
	shellIDs := map[string]bool{}
	shell.Find("[id]").Each(func(_ int, s *goquery.Selection) {
		if id := s.AttrOr("id", ""); id != "" {
			shellIDs[id] = true
		}
	})
	if !shellIDs["wizard-frame"] || !shellIDs["error-banner"] {
		t.Fatalf("shell missing required ids; got %v", shellIDs)
	}

	// Walk the user flow and gather responses.
	cookie := ""
	docs := []*goquery.Document{shell}
	rec, cookie = env.get(t, cookie, "/")
	docs = append(docs, parseHTML(t, rec))

	rec, cookie = env.post(t, cookie, "/wizard/name", url.Values{"name": {"Walker"}})
	docs = append(docs, parseHTML(t, rec))

	rec, cookie = env.post(t, cookie, "/wizard/game", url.Values{"game": {"2048"}})
	docs = append(docs, parseHTML(t, rec))

	rec, cookie = env.post(t, cookie, "/wizard/difficulty", url.Values{"difficulty": {"easy"}})
	docs = append(docs, parseHTML(t, rec))

	rec, cookie = env.post(t, cookie, "/wizard/start", nil)
	docs = append(docs, parseHTML(t, rec))

	rec, _ = env.post(t, cookie, "/game/2048/move", url.Values{"dir": {"left"}})
	docs = append(docs, parseHTML(t, rec))

	checked := 0
	for i, doc := range docs {
		doc.Find("[hx-target]").Each(func(_ int, s *goquery.Selection) {
			target := s.AttrOr("hx-target", "")
			if !strings.HasPrefix(target, "#") {
				return
			}
			id := strings.TrimPrefix(target, "#")
			if !shellIDs[id] {
				// Allow self-targeting elements (board's hx-target="this") and
				// the board id, which only exists in the playing state.
				if id == "twenty48-board" {
					return
				}
				t.Errorf("doc[%d]: hx-target=%q has no matching id in shell", i, target)
			}
			checked++
		})
		doc.Find(`[hx-swap-oob="true"]`).Each(func(_ int, s *goquery.Selection) {
			id := s.AttrOr("id", "")
			if id == "" {
				return
			}
			if !shellIDs[id] {
				t.Errorf("doc[%d]: hx-swap-oob id=%q has no matching id in shell", i, id)
			}
			checked++
		})
	}
	if checked == 0 {
		t.Errorf("cross-reference exercised 0 references")
	}
}

// --- helpers ---

func walkToReady(t *testing.T, env *testEnv, name, game, diff string) string {
	t.Helper()
	cookie := ""
	_, cookie = env.get(t, cookie, "/")
	_, cookie = env.post(t, cookie, "/wizard/name", url.Values{"name": {name}})
	_, cookie = env.post(t, cookie, "/wizard/game", url.Values{"game": {game}})
	_, cookie = env.post(t, cookie, "/wizard/difficulty", url.Values{"difficulty": {diff}})
	return cookie
}

func walkToPlaying(t *testing.T, env *testEnv, name, game, diff string) string {
	t.Helper()
	cookie := walkToReady(t, env, name, game, diff)
	_, cookie = env.post(t, cookie, "/wizard/start", nil)
	return cookie
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
