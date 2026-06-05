package seed

// This file generates Razorpay-shaped entity ids deterministically. Real
// Razorpay ids are a type prefix (pay_, rfnd_, setl_, disp_, order_) followed by
// a 14-character base62 token (e.g. "pay_29QQjUNkbFbiY8"). We reproduce that
// SHAPE so fixtures look like real API objects, but the token is drawn from the
// seeder's deterministic RNG — same (world, period) => same ids — rather than
// being random per run (SPEC §2, §12).

const (
	prefixPayment    = "pay_"
	prefixRefund     = "rfnd_"
	prefixSettlement = "setl_"
	prefixDispute    = "disp_"
	prefixOrder      = "order_"

	// idTokenLen is the length of the base62 token following the prefix, matching
	// Razorpay's 14-character ids.
	idTokenLen = 14
)

// base62Alphabet is the digit set for the id token, in fixed order so the
// mapping from RNG draws to characters is stable.
const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// idGen mints deterministic, prefixed, Razorpay-shaped ids from an RNG. It holds
// the RNG so every id in a generation run is drawn from the one seeded stream,
// keeping the whole substrate reproducible. It is not safe for concurrent use
// (the generator runs single-threaded, preserving draw order / determinism).
type idGen struct {
	rng *RNG
}

// newIDGen builds an id generator over the given RNG.
func newIDGen(rng *RNG) *idGen { return &idGen{rng: rng} }

// token draws a fresh idTokenLen-character base62 token from the RNG. Each
// character is one IntRange draw over the alphabet, so the token consumes a
// fixed, known number of draws — important for keeping the stream aligned across
// runs.
func (g *idGen) token() string {
	b := make([]byte, idTokenLen)
	for i := range b {
		b[i] = base62Alphabet[g.rng.IntRange(0, len(base62Alphabet)-1)]
	}
	return string(b)
}

// payment, refund, settlement, dispute, order mint a fresh id with the matching
// prefix.
func (g *idGen) payment() string    { return prefixPayment + g.token() }
func (g *idGen) refund() string     { return prefixRefund + g.token() }
func (g *idGen) settlement() string { return prefixSettlement + g.token() }
func (g *idGen) dispute() string    { return prefixDispute + g.token() }
func (g *idGen) order() string      { return prefixOrder + g.token() }
