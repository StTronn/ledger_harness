package seed

import (
	"hash/fnv"
	"math/rand"
)

// SeedFor derives a STABLE 64-bit seed from (world, period). The same pair
// always yields the same seed and therefore the same RNG stream — there is no
// wall clock, no process entropy, no os.Getpid (SPEC §2, §12: "seeded RNG from
// world+period; no wall-clock").
//
// FNV-1a is used because it is a deterministic, well-mixed, dependency-free hash
// in the standard library; the exact algorithm does not matter for correctness
// as long as it is fixed, but pinning it (rather than, say, maphash which is
// process-seeded) is what guarantees reproducibility across runs and machines.
func SeedFor(world, period string) int64 {
	h := fnv.New64a()
	// Separate the fields with a NUL so ("ab","c") and ("a","bc") hash
	// differently — otherwise concatenation would collide.
	_, _ = h.Write([]byte(world))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(period))
	return int64(h.Sum64())
}

// RNG is the seeder's deterministic random source: a thin wrapper over
// *math/rand.Rand that exposes only the small, integer-valued draws the
// generator needs. It deliberately offers NO float method, so no money or
// quantity can ever be derived through a float (the money invariant).
//
// All draws are reproducible: given the same seed, the same sequence of method
// calls returns the same values. Callers must therefore make their draws in a
// fixed order (the generator does), since reordering draws changes the stream.
type RNG struct {
	r *rand.Rand
}

// NewRNG builds an RNG seeded deterministically from (world, period).
func NewRNG(world, period string) *RNG {
	return &RNG{r: rand.New(rand.NewSource(SeedFor(world, period)))}
}

// NewRNGFromSeed builds an RNG from an explicit seed. Used by tests that want to
// pin a stream without going through (world, period).
func NewRNGFromSeed(seed int64) *RNG {
	return &RNG{r: rand.New(rand.NewSource(seed))}
}

// IntRange returns a uniformly random integer in the inclusive range [lo, hi].
// It panics if lo > hi, which is a programming error in the generator's rules
// (a backwards range), not a runtime condition to recover from.
func (g *RNG) IntRange(lo, hi int) int {
	if lo > hi {
		panic("seed: IntRange called with lo > hi")
	}
	if lo == hi {
		return lo
	}
	return lo + g.r.Intn(hi-lo+1)
}

// Int64Range returns a uniformly random int64 in the inclusive range [lo, hi].
// It is the money-amount draw: amounts are paise (int64), drawn directly in
// integer space so no float is involved. Panics on a backwards range.
func (g *RNG) Int64Range(lo, hi int64) int64 {
	if lo > hi {
		panic("seed: Int64Range called with lo > hi")
	}
	if lo == hi {
		return lo
	}
	// Int63n's argument is the exclusive size of [0, n); +1 makes hi inclusive.
	return lo + g.r.Int63n(hi-lo+1)
}

// Chance reports true with probability numerator/denominator (e.g. Chance(1, 20)
// is a 5% chance). It is implemented as an integer draw so probabilities stay
// float-free and deterministic. Panics if denominator <= 0 or numerator < 0.
func (g *RNG) Chance(numerator, denominator int) bool {
	if denominator <= 0 || numerator < 0 {
		panic("seed: Chance called with non-positive denominator or negative numerator")
	}
	return g.r.Intn(denominator) < numerator
}

// Pick returns a uniformly random element of options. It panics on an empty
// slice (the generator always passes a non-empty option set).
func (g *RNG) Pick(options []string) string {
	if len(options) == 0 {
		panic("seed: Pick called with no options")
	}
	return options[g.r.Intn(len(options))]
}
