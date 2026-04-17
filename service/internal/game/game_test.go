package game_test

import (
	"math"
	"testing"

	"conway-life/internal/game"
)

// cellAt returns the age value at column x, row y.
func cellAt(g *game.Game, x, y int) uint16 {
	return g.Cells[y][x]
}

// liveCount returns the number of live cells (age > 0) on the board.
func liveCount(g *game.Game) int {
	n := 0
	for y := range g.Cells {
		for _, v := range g.Cells[y] {
			if v > 0 {
				n++
			}
		}
	}
	return n
}

func makeGame(t *testing.T, width, height int, liveCells [][2]int) *game.Game {
	t.Helper()
	g, err := game.NewGame(width, height, liveCells)
	if err != nil {
		t.Fatalf("NewGame(%d,%d,...): %v", width, height, err)
	}
	return g
}

// ---------------------------------------------------------------------------
// NewGame validation
// ---------------------------------------------------------------------------

func TestNewGame_EmptyCells(t *testing.T) {
	g := makeGame(t, 5, 5, nil)
	if g.Width != 5 || g.Height != 5 {
		t.Errorf("dims: got %dx%d, want 5x5", g.Width, g.Height)
	}
	if g.Generation != 0 {
		t.Errorf("generation: got %d, want 0", g.Generation)
	}
	if liveCount(g) != 0 {
		t.Errorf("expected all-dead board, got %d live cells", liveCount(g))
	}
}

func TestNewGame_WithCells(t *testing.T) {
	// Three live cells forming a horizontal blinker
	g := makeGame(t, 5, 5, [][2]int{{1, 2}, {2, 2}, {3, 2}})
	if cellAt(g, 1, 2) != 1 {
		t.Errorf("cell (1,2): got %d, want 1", cellAt(g, 1, 2))
	}
	if cellAt(g, 2, 2) != 1 {
		t.Errorf("cell (2,2): got %d, want 1", cellAt(g, 2, 2))
	}
	if cellAt(g, 3, 2) != 1 {
		t.Errorf("cell (3,2): got %d, want 1", cellAt(g, 3, 2))
	}
	if liveCount(g) != 3 {
		t.Errorf("live count: got %d, want 3", liveCount(g))
	}
}

func TestNewGame_DuplicateCellsIgnored(t *testing.T) {
	g := makeGame(t, 5, 5, [][2]int{{1, 1}, {1, 1}, {2, 2}})
	if liveCount(g) != 2 {
		t.Errorf("live count: got %d, want 2 (duplicates should be deduplicated)", liveCount(g))
	}
}

func TestNewGame_InitialCellAge(t *testing.T) {
	g := makeGame(t, 5, 5, [][2]int{{0, 0}})
	if cellAt(g, 0, 0) != 1 {
		t.Errorf("initial cell age: got %d, want 1", cellAt(g, 0, 0))
	}
}

func TestNewGame_OutOfBoundsReturnsError(t *testing.T) {
	tests := []struct {
		name  string
		cells [][2]int
		w, h  int
	}{
		{"x equals width", [][2]int{{5, 0}}, 5, 5},
		{"x negative", [][2]int{{-1, 0}}, 5, 5},
		{"y equals height", [][2]int{{0, 5}}, 5, 5},
		{"y negative", [][2]int{{0, -1}}, 5, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := game.NewGame(tc.w, tc.h, tc.cells)
			if err == nil {
				t.Error("expected error for out-of-bounds cell, got nil")
			}
		})
	}
}

func TestNewGame_BoardDimensions(t *testing.T) {
	g := makeGame(t, 3, 7, nil)
	if len(g.Cells) != 7 {
		t.Errorf("len(Cells) (rows): got %d, want 7", len(g.Cells))
	}
	if len(g.Cells[0]) != 3 {
		t.Errorf("len(Cells[0]) (cols): got %d, want 3", len(g.Cells[0]))
	}
}

// ---------------------------------------------------------------------------
// Step — empty and single-cell boards
// ---------------------------------------------------------------------------

func TestStep_EmptyBoard(t *testing.T) {
	g := makeGame(t, 5, 5, nil)
	g.Step()
	if g.Generation != 1 {
		t.Errorf("generation: got %d, want 1", g.Generation)
	}
	if liveCount(g) != 0 {
		t.Errorf("expected empty board after step, got %d live cells", liveCount(g))
	}
}

func TestStep_SingleLiveCell_Dies(t *testing.T) {
	g := makeGame(t, 5, 5, [][2]int{{2, 2}})
	g.Step()
	if liveCount(g) != 0 {
		t.Errorf("single cell should die: got %d live cells", liveCount(g))
	}
	if cellAt(g, 2, 2) != 0 {
		t.Errorf("cell (2,2): got %d, want 0 (dead)", cellAt(g, 2, 2))
	}
}

func TestStep_1x1_LiveCell_Dies(t *testing.T) {
	g := makeGame(t, 1, 1, [][2]int{{0, 0}})
	g.Step()
	if cellAt(g, 0, 0) != 0 {
		t.Errorf("1x1 live cell should die, got age %d", cellAt(g, 0, 0))
	}
	if g.Generation != 1 {
		t.Errorf("generation: got %d, want 1", g.Generation)
	}
}

func TestStep_1x1_DeadCell_StaysDead(t *testing.T) {
	g := makeGame(t, 1, 1, nil)
	g.Step()
	if cellAt(g, 0, 0) != 0 {
		t.Errorf("1x1 dead cell should stay dead, got age %d", cellAt(g, 0, 0))
	}
}

func TestStep_GenerationIncrements(t *testing.T) {
	g := makeGame(t, 5, 5, nil)
	for i := 1; i <= 5; i++ {
		g.Step()
		if g.Generation != i {
			t.Errorf("after step %d: generation=%d, want %d", i, g.Generation, i)
		}
	}
}

// ---------------------------------------------------------------------------
// Step — Conway rules: birth and death
// ---------------------------------------------------------------------------

// Dead cell with exactly 3 live neighbors becomes alive (age 1).
// Arrangement on 5x5: three cells in a column share a common dead neighbor.
//
//	. . . . .
//	. X . . .   (1,1)
//	. X . . .   (1,2) ← the dead cell (0,2) has neighbors (0,1),(1,1),(0,3),(1,3),(1,2) — 3 live? No.
//
// Use a simpler T-shape: cells at (1,0),(1,1),(1,2). The dead cell (0,1) has neighbors
// (0,0)=0,(1,0)=1,(0,1)? wait that's itself. Let me use coords carefully.
//
// Horizontal triple at y=2: (1,2),(2,2),(3,2). Dead cell at (2,1): neighbors are
// (1,1)=0,(2,1)? that's itself. Neighbors of (2,1): (1,0),(2,0),(3,0),(1,1),(3,1),(1,2),(2,2),(3,2) = 3 live → birth.
func TestStep_DeadCellWith3Neighbors_BecomesAlive(t *testing.T) {
	// Horizontal blinker: (1,2),(2,2),(3,2) on a 5x5 board.
	// Cell (2,1) has exactly 3 live neighbors and should be born.
	g := makeGame(t, 5, 5, [][2]int{{1, 2}, {2, 2}, {3, 2}})
	g.Step()
	if cellAt(g, 2, 1) != 1 {
		t.Errorf("dead cell (2,1) with 3 neighbors: got age %d, want 1 (born)", cellAt(g, 2, 1))
	}
}

// Live cell with 2 live neighbors survives, age increments.
func TestStep_LiveCellWith2Neighbors_Survives(t *testing.T) {
	// Horizontal blinker: (1,2),(2,2),(3,2).
	// Center cell (2,2) has 2 live neighbors (1,2) and (3,2) → survives.
	g := makeGame(t, 5, 5, [][2]int{{1, 2}, {2, 2}, {3, 2}})
	// Initial age of center is 1.
	g.Step()
	if cellAt(g, 2, 2) != 2 {
		t.Errorf("center cell after step: got age %d, want 2 (survived, age incremented)", cellAt(g, 2, 2))
	}
}

// Live cell with 3 live neighbors survives, age increments.
func TestStep_LiveCellWith3Neighbors_Survives(t *testing.T) {
	// 2x2 block at (1,1),(2,1),(1,2),(2,2). Each cell has exactly 3 neighbors → all survive.
	g := makeGame(t, 4, 4, [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}})
	g.Step()
	// All four cells should survive with age 2.
	for _, pos := range [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}} {
		age := cellAt(g, pos[0], pos[1])
		if age != 2 {
			t.Errorf("block cell (%d,%d): got age %d, want 2", pos[0], pos[1], age)
		}
	}
}

// Live cell with 0 neighbors dies.
func TestStep_LiveCellWith0Neighbors_Dies(t *testing.T) {
	g := makeGame(t, 5, 5, [][2]int{{2, 2}})
	g.Step()
	if cellAt(g, 2, 2) != 0 {
		t.Errorf("isolated cell: got age %d, want 0 (dead)", cellAt(g, 2, 2))
	}
}

// Live cell with 1 neighbor dies.
func TestStep_LiveCellWith1Neighbor_Dies(t *testing.T) {
	g := makeGame(t, 5, 5, [][2]int{{2, 2}, {2, 3}})
	g.Step()
	// Both cells have only 1 neighbor each → both die.
	if cellAt(g, 2, 2) != 0 {
		t.Errorf("cell (2,2) with 1 neighbor: got age %d, want 0", cellAt(g, 2, 2))
	}
	if cellAt(g, 2, 3) != 0 {
		t.Errorf("cell (2,3) with 1 neighbor: got age %d, want 0", cellAt(g, 2, 3))
	}
}

// Live cell with 4+ neighbors dies (overpopulation).
func TestStep_LiveCellWith4Neighbors_Dies(t *testing.T) {
	// Plus-shape: center (2,2) surrounded by 4 neighbors.
	g := makeGame(t, 5, 5, [][2]int{{2, 1}, {1, 2}, {2, 2}, {3, 2}, {2, 3}})
	g.Step()
	// Center (2,2) has 4 live neighbors → dies.
	if cellAt(g, 2, 2) != 0 {
		t.Errorf("over-populated center (2,2): got age %d, want 0 (dead)", cellAt(g, 2, 2))
	}
}

// ---------------------------------------------------------------------------
// Step — edge / boundary behavior (no toroidal wrapping)
// ---------------------------------------------------------------------------

// Corner cell at (0,0) has only 3 possible neighbors (right, below, diag).
// Place 3 live cells adjacent to (0,0): if they give it exactly 3 live neighbors, it should be born.
func TestStep_EdgeCell_CornerBirth(t *testing.T) {
	// Cells: (1,0),(0,1),(1,1) — all three neighbors of corner (0,0) on a hard-bounded board.
	g := makeGame(t, 5, 5, [][2]int{{1, 0}, {0, 1}, {1, 1}})
	g.Step()
	if cellAt(g, 0, 0) != 1 {
		t.Errorf("corner (0,0) with 3 in-bounds neighbors: got age %d, want 1 (born)", cellAt(g, 0, 0))
	}
}

// Cell at the right edge does not wrap around to the left.
func TestStep_EdgeCell_NoWrap(t *testing.T) {
	// If the board wrapped, a cell at x=4 (rightmost col, 5-wide) with a neighbor
	// at x=0 would see an extra live neighbor. Place a single cell at (4,2) and
	// a cell at (0,2). They should NOT be neighbors.
	g := makeGame(t, 5, 5, [][2]int{{4, 2}, {0, 2}})
	g.Step()
	// Both cells should die — they have only 1 neighbor each (each other's presence
	// doesn't count; they're not adjacent without wrapping).
	if cellAt(g, 4, 2) != 0 {
		t.Errorf("right-edge cell with non-adjacent partner: got age %d, want 0 (dead)", cellAt(g, 4, 2))
	}
}

// 1×5 strip — test neighbor counting on a 1-wide board.
// Board: width=1 height=5, live cells at (x=0,y=1),(x=0,y=2),(x=0,y=3).
// Neighbor analysis (Moore, hard edges, no wrap):
//   (0,1): neighbors = (0,0)=dead, (0,2)=alive → 1 neighbor → dies
//   (0,2): neighbors = (0,1)=alive, (0,3)=alive → 2 neighbors → survives
//   (0,3): neighbors = (0,2)=alive, (0,4)=dead → 1 neighbor → dies
//   (0,0),(0,4): 1 neighbor each → no birth
// Expected after 1 step: only center cell (0,2) alive.
func TestStep_ThinBoard_1x5(t *testing.T) {
	g := makeGame(t, 1, 5, [][2]int{{0, 1}, {0, 2}, {0, 3}})
	g.Step()
	if liveCount(g) != 1 {
		t.Errorf("1x5 strip after step: expected 1 live cell (center), got %d", liveCount(g))
	}
	if cellAt(g, 0, 2) == 0 {
		t.Errorf("center cell (0,2): expected alive (2 neighbors), got dead")
	}
	if cellAt(g, 0, 1) != 0 {
		t.Errorf("end cell (0,1): expected dead (1 neighbor), got age %d", cellAt(g, 0, 1))
	}
	if cellAt(g, 0, 3) != 0 {
		t.Errorf("end cell (0,3): expected dead (1 neighbor), got age %d", cellAt(g, 0, 3))
	}
}

// ---------------------------------------------------------------------------
// Step — still life: 2×2 block
// ---------------------------------------------------------------------------

func TestStep_Block_StillLife(t *testing.T) {
	g := makeGame(t, 6, 6, [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}})
	for step := 1; step <= 5; step++ {
		g.Step()
		// Block cells should all survive with age == step+1 (initial age 1).
		for _, pos := range [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}} {
			want := uint16(step + 1)
			got := cellAt(g, pos[0], pos[1])
			if got != want {
				t.Errorf("step %d, block cell (%d,%d): got age %d, want %d", step, pos[0], pos[1], got, want)
			}
		}
		// No new births outside the block.
		if liveCount(g) != 4 {
			t.Errorf("step %d: block live count = %d, want 4", step, liveCount(g))
		}
	}
}

// ---------------------------------------------------------------------------
// Step — oscillator: horizontal blinker (period 2)
// ---------------------------------------------------------------------------

func TestStep_Blinker_Oscillates(t *testing.T) {
	// Horizontal blinker: cells at (1,2),(2,2),(3,2) on a 5x5 board.
	g := makeGame(t, 5, 5, [][2]int{{1, 2}, {2, 2}, {3, 2}})

	// After step 1 → vertical blinker: (2,1),(2,2),(2,3)
	g.Step()
	if liveCount(g) != 3 {
		t.Errorf("blinker step 1: live count = %d, want 3", liveCount(g))
	}
	for _, pos := range [][2]int{{2, 1}, {2, 2}, {2, 3}} {
		if cellAt(g, pos[0], pos[1]) == 0 {
			t.Errorf("blinker step 1: expected live at (%d,%d)", pos[0], pos[1])
		}
	}
	for _, pos := range [][2]int{{1, 2}, {3, 2}} {
		if cellAt(g, pos[0], pos[1]) != 0 {
			t.Errorf("blinker step 1: expected dead at (%d,%d), got age %d", pos[0], pos[1], cellAt(g, pos[0], pos[1]))
		}
	}

	// Center cell (2,2) survived — age should be 2.
	if cellAt(g, 2, 2) != 2 {
		t.Errorf("blinker step 1: center age = %d, want 2", cellAt(g, 2, 2))
	}
	// Outer vertical cells (2,1) and (2,3) are newborns — age 1.
	if cellAt(g, 2, 1) != 1 {
		t.Errorf("blinker step 1: (2,1) age = %d, want 1", cellAt(g, 2, 1))
	}
	if cellAt(g, 2, 3) != 1 {
		t.Errorf("blinker step 1: (2,3) age = %d, want 1", cellAt(g, 2, 3))
	}

	// After step 2 → back to horizontal blinker: (1,2),(2,2),(3,2)
	g.Step()
	if liveCount(g) != 3 {
		t.Errorf("blinker step 2: live count = %d, want 3", liveCount(g))
	}
	for _, pos := range [][2]int{{1, 2}, {2, 2}, {3, 2}} {
		if cellAt(g, pos[0], pos[1]) == 0 {
			t.Errorf("blinker step 2: expected live at (%d,%d)", pos[0], pos[1])
		}
	}
	// Center (2,2) survived both steps — age should be 3.
	if cellAt(g, 2, 2) != 3 {
		t.Errorf("blinker step 2: center age = %d, want 3", cellAt(g, 2, 2))
	}
	// Outer horizontal cells are reborn — age 1.
	if cellAt(g, 1, 2) != 1 {
		t.Errorf("blinker step 2: (1,2) age = %d, want 1", cellAt(g, 1, 2))
	}
	if cellAt(g, 3, 2) != 1 {
		t.Errorf("blinker step 2: (3,2) age = %d, want 1", cellAt(g, 3, 2))
	}
}

// ---------------------------------------------------------------------------
// Step — full 3×3 board (§9.4)
// ---------------------------------------------------------------------------

// All 9 cells alive. After one step:
// - Corners (3 neighbors each) → survive (age 2)
// - Edges (5 neighbors each) → die (overpopulation)
// - Center (8 neighbors) → dies (overpopulation)
func TestStep_Full3x3Board(t *testing.T) {
	all := [][2]int{
		{0, 0}, {1, 0}, {2, 0},
		{0, 1}, {1, 1}, {2, 1},
		{0, 2}, {1, 2}, {2, 2},
	}
	g := makeGame(t, 3, 3, all)
	g.Step()

	corners := [][2]int{{0, 0}, {2, 0}, {0, 2}, {2, 2}}
	for _, c := range corners {
		if cellAt(g, c[0], c[1]) != 2 {
			t.Errorf("corner (%d,%d): got age %d, want 2 (survived)", c[0], c[1], cellAt(g, c[0], c[1]))
		}
	}
	edges := [][2]int{{1, 0}, {0, 1}, {2, 1}, {1, 2}}
	for _, e := range edges {
		if cellAt(g, e[0], e[1]) != 0 {
			t.Errorf("edge (%d,%d): got age %d, want 0 (dead)", e[0], e[1], cellAt(g, e[0], e[1]))
		}
	}
	if cellAt(g, 1, 1) != 0 {
		t.Errorf("center (1,1): got age %d, want 0 (dead)", cellAt(g, 1, 1))
	}
	if liveCount(g) != 4 {
		t.Errorf("live count after full-3x3 step: got %d, want 4 (corners only)", liveCount(g))
	}
}

// ---------------------------------------------------------------------------
// Step — age cap at uint16 max (65535)
// ---------------------------------------------------------------------------

func TestStep_AgeCapAtUint16Max(t *testing.T) {
	// 2x2 block — each cell has 3 live neighbors, so all survive every step.
	// Directly mutate Cells to an age near the uint16 cap and verify the
	// step algorithm clamps to math.MaxUint16 rather than overflowing to 0.
	g := makeGame(t, 4, 4, [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}})

	// Set one block cell to MaxUint16-1; the others stay at 1.
	g.Cells[1][1] = math.MaxUint16 - 1

	// Step 1: cell at MaxUint16-1 should advance to MaxUint16.
	g.Step()
	if g.Cells[1][1] != math.MaxUint16 {
		t.Errorf("age after step from MaxUint16-1: got %d, want %d (MaxUint16)", g.Cells[1][1], uint16(math.MaxUint16))
	}

	// Step 2: cell at MaxUint16 must stay capped — not overflow to 0.
	g.Step()
	if g.Cells[1][1] != math.MaxUint16 {
		t.Errorf("age after step from MaxUint16: got %d, want %d (capped, no overflow)", g.Cells[1][1], uint16(math.MaxUint16))
	}
}

// ---------------------------------------------------------------------------
// Step — glider translates (+1,+1) after 4 steps on a large enough board
// ---------------------------------------------------------------------------
//
// Classic glider (in x,y coords where y increases downward):
//
//	. X .
//	. . X
//	X X X
//
// Encoded as: (1,0),(2,1),(0,2),(1,2),(2,2)
// After 4 steps on an unbounded region, the glider moves +1 in x, +1 in y.
func TestStep_Glider_Translates(t *testing.T) {
	// Place glider with enough clearance from edges so it doesn't die.
	// Start at offset (2,2) on a 12x12 board.
	offsetX, offsetY := 2, 2
	initial := [][2]int{
		{offsetX + 1, offsetY + 0},
		{offsetX + 2, offsetY + 1},
		{offsetX + 0, offsetY + 2},
		{offsetX + 1, offsetY + 2},
		{offsetX + 2, offsetY + 2},
	}
	g := makeGame(t, 12, 12, initial)
	for i := 0; i < 4; i++ {
		g.Step()
	}

	// After 4 steps, the glider should occupy the same shape shifted by (+1,+1).
	expected := [][2]int{
		{offsetX + 2, offsetY + 1},
		{offsetX + 3, offsetY + 2},
		{offsetX + 1, offsetY + 3},
		{offsetX + 2, offsetY + 3},
		{offsetX + 3, offsetY + 3},
	}
	for _, pos := range expected {
		if cellAt(g, pos[0], pos[1]) == 0 {
			t.Errorf("glider after 4 steps: expected live at (%d,%d)", pos[0], pos[1])
		}
	}
	if liveCount(g) != 5 {
		t.Errorf("glider after 4 steps: live count = %d, want 5", liveCount(g))
	}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

func TestRegistry_PutAndGet(t *testing.T) {
	r := game.NewRegistry()
	g := makeGame(t, 5, 5, nil)
	r.Put(g)
	got, ok := r.Get(g.ID)
	if !ok {
		t.Fatal("Get: expected true, got false")
	}
	if got.ID != g.ID {
		t.Errorf("Get: ID mismatch: got %q, want %q", got.ID, g.ID)
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	r := game.NewRegistry()
	_, ok := r.Get("nonexistent-id")
	if ok {
		t.Error("Get on unknown id: expected false, got true")
	}
}
