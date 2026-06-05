package seed

import "testing"

// TestSeedForStable asserts the seed derived from (world, period) is stable and
// field-separated: the same pair always yields the same seed, and swapping the
// field boundary changes it (so ("ab","c") != ("a","bc")).
func TestSeedForStable(t *testing.T) {
	a := SeedFor("dtc", "2026-05")
	b := SeedFor("dtc", "2026-05")
	if a != b {
		t.Fatalf("SeedFor not stable: %d != %d", a, b)
	}
	if SeedFor("ab", "c") == SeedFor("a", "bc") {
		t.Error("SeedFor must field-separate world/period to avoid concatenation collisions")
	}
	if SeedFor("dtc", "2026-05") == SeedFor("dtc", "2026-06") {
		t.Error("SeedFor must differ across periods")
	}
}

// TestRNGDeterministicStream asserts two RNGs seeded from the same (world,
// period) produce identical draw sequences, and differ from another period.
func TestRNGDeterministicStream(t *testing.T) {
	draw := func(g *RNG) []int64 {
		out := make([]int64, 0, 30)
		for i := 0; i < 10; i++ {
			out = append(out, int64(g.IntRange(0, 1000)))
			out = append(out, g.Int64Range(0, 1_000_000))
			b := 0
			if g.Chance(1, 2) {
				b = 1
			}
			out = append(out, int64(b))
		}
		return out
	}
	a := draw(NewRNG("dtc", "2026-05"))
	b := draw(NewRNG("dtc", "2026-05"))
	if len(a) != len(b) {
		t.Fatalf("length mismatch")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("stream diverged at %d: %d != %d", i, a[i], b[i])
		}
	}
	c := draw(NewRNG("dtc", "2026-06"))
	same := true
	for i := range a {
		if a[i] != c[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different period produced identical stream — seed is not period-sensitive")
	}
}

// TestRNGRangesInclusive asserts IntRange / Int64Range honor inclusive bounds and
// degenerate (lo==hi) ranges, and that Chance(0,n)/Chance(n,n) are deterministic.
func TestRNGRangesInclusive(t *testing.T) {
	g := NewRNGFromSeed(42)
	for i := 0; i < 1000; i++ {
		if v := g.IntRange(5, 7); v < 5 || v > 7 {
			t.Fatalf("IntRange(5,7) out of range: %d", v)
		}
		if v := g.Int64Range(-3, 3); v < -3 || v > 3 {
			t.Fatalf("Int64Range(-3,3) out of range: %d", v)
		}
	}
	if g.IntRange(9, 9) != 9 {
		t.Error("IntRange(9,9) must be 9")
	}
	if g.Int64Range(2, 2) != 2 {
		t.Error("Int64Range(2,2) must be 2")
	}
	if g.Chance(0, 5) {
		t.Error("Chance(0,5) must be false")
	}
	if !g.Chance(5, 5) {
		t.Error("Chance(5,5) must be true")
	}
}
