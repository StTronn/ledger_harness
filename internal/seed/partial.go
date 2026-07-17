package seed

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/money"
)

// partial.go implements the PARTIAL-REFUNDS world (the "judgment" period): with
// Options.PartialRefunds on, every order carries two same-rate line items and the
// first three refunds the generator mints become the judgment spectrum the §8
// classify agent must navigate:
//
//	R1 "item-match"  — amount == the order's items[0]; truth books refund_reversal
//	                   at the item's rate (the agent CAN book this: exact line match).
//	R2 "goodwill"    — matches no item; notes.reason = "goodwill"; truth books
//	                   price_adjustment (a credit note). Agent POLICY: goodwill is a
//	                   human call — it escalates, never books it.
//	R3 "unexplained" — matches no item, no annotation; truth books price_adjustment
//	                   (it WAS goodwill — information the system is never given), so
//	                   the only correct system behavior is to escalate. This is the
//	                   designed honest sub-100% of the period.
//
// Unlike the inject.go break classes (post-generation perturbations with truth
// untouched), partial refunds change the WORLD: the refund amounts flow through
// batch netting -> deposit -> bank feed, and truth books different entry types
// per class. So they are applied AT GENERATION TIME, keeping every downstream
// artifact consistent by construction. No new RNG draws are made (designation is
// a counter; amounts are arithmetic on the parent payment), so the option
// perturbs nothing about the clean stream it doesn't own.

// PartialClass names which judgment-spectrum slot a designated refund fills.
type PartialClass string

const (
	PartialItemMatch   PartialClass = "item-match"
	PartialGoodwill    PartialClass = "goodwill"
	PartialUnexplained PartialClass = "unexplained"
)

// partialClasses is the designation order: the first refund the generator mints
// becomes R1, the second R2, the third R3.
var partialClasses = []PartialClass{PartialItemMatch, PartialGoodwill, PartialUnexplained}

// PartialRefund records one designated refund for the CLI summary and tests.
type PartialRefund struct {
	RefundID  string
	PaymentID string
	OrderID   string
	Class     PartialClass
}

// PartialResult is the partial-refunds outcome carried on Results. Empty when
// the option is off.
type PartialResult struct {
	Refunds []PartialRefund
}

// orderItemSplit derives the two line-item amounts for an order from its total —
// items[0] = amount*2/5, items[1] = the remainder — deterministically and with no
// RNG draw. The 2/5 share keeps both items strictly positive for any realistic
// amount and makes R1's amount (== items[0]) strictly partial.
func orderItemSplit(amount money.Money) (item0, item1 money.Money) {
	a := amount.Paise()
	i0 := a * 2 / 5
	return money.FromPaise(i0), money.FromPaise(a - i0)
}

// partialAmountFor returns the designated refund amount for a class, derived
// arithmetically from the parent payment's gross. The goodwill/unexplained
// formulas are chosen so they can never collide with either line item
// (item0 = 2a/5, item1 = 3a/5): 3a/10+1 == 2a/5 would need a == 10 paise and
// 3a/10+1 == 3a/5 would need a ≈ 3 paise — both impossible for realistic
// amounts, and guarded at designation time anyway.
func partialAmountFor(class PartialClass, parent money.Money) money.Money {
	a := parent.Paise()
	switch class {
	case PartialItemMatch:
		i0, _ := orderItemSplit(parent)
		return i0
	case PartialGoodwill:
		return money.FromPaise(a*3/10 + 1)
	case PartialUnexplained:
		return money.FromPaise(a/4 + 3)
	default:
		panic("seed: unknown partial class " + string(class))
	}
}

// designatePartial converts the freshly minted full refund rf into the next
// partial-refund designation, mutating its amount (and, for goodwill, its reason
// annotation). It returns the class so the caller books the right truth entry.
// It errors if the derived amount collides with a line item it must not match or
// escapes (0, parent) — a generation-shape regression to fail loudly on, never a
// silently-wrong world.
func (g *generator) designatePartial(rf *Refund, pay Payment) (PartialClass, error) {
	class := partialClasses[len(g.partialOut)]
	amt := partialAmountFor(class, pay.Amount)
	if amt.Paise() <= 0 || amt.Paise() >= pay.Amount.Paise() {
		return "", fmt.Errorf("seed: partial %s amount %s escapes (0, %s)", class, amt, pay.Amount)
	}
	i0, i1 := orderItemSplit(pay.Amount)
	switch class {
	case PartialItemMatch:
		if amt != i0 {
			return "", fmt.Errorf("seed: partial item-match amount %s != items[0] %s", amt, i0)
		}
	case PartialGoodwill, PartialUnexplained:
		if amt == i0 || amt == i1 {
			return "", fmt.Errorf("seed: partial %s amount %s collides with a line item", class, amt)
		}
	}

	rf.Amount = amt
	if class == PartialGoodwill {
		rf.Notes.Reason = "goodwill"
	}
	g.partialOut = append(g.partialOut, PartialRefund{
		RefundID:  rf.ID,
		PaymentID: pay.ID,
		OrderID:   pay.OrderID,
		Class:     class,
	})
	return class, nil
}
