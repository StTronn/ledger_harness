// Package reconcile implements the SPEC §7 three-check reconciliation of a
// posted ledger against the independent settlement and bank-feed records. It
// runs AFTER every event has posted (SPEC §5): each check that fails produces a
// Break carrying enough context for the (Phase-8) investigate agent — the
// settlement it concerns, expected vs actual money, and the candidate event ids
// it should look at. There is NO agent in Phase 5: Reconcile DETECTS and LISTS
// breaks; nothing is resolved or mutated.
//
// # Purity (SPEC §6, §12)
//
// Reconcile is a PURE function of its inputs: the receivable balance at period
// end (a money value derived from the posted ledger), the raw settlements, the
// raw payments/refunds (the batch members), and the independent bank feed. It
// reads no file, no wall clock, and no randomness — the same inputs always
// yield the same ordered slice of breaks. The caller (the close orchestrator)
// supplies the ledger-derived balance; reconcile never reaches into the ledger's
// internals, so it stays a value-in / value-out function that tests can drive
// with hand-built scenarios.
//
// # Boundaries (SPEC §4.4, §12 truth isolation)
//
// reconcile MUST NOT read or import internal/truth or internal/seed. It consumes
// only the AGENT-INPUT records (settlements, payments, refunds, bank feed) plus
// the ledger-derived receivable balance. The truth-isolation guard test policies
// the import graph; reconcile imports only internal/money and internal/ingest
// (for the raw record shapes).
//
// # Money invariant (SPEC §1, §4)
//
// Every amount is integer minor units — paise — as internal/money.Money (int64).
// No float type touches money in this package; the comparisons in every check
// are exact integer equalities (no tolerance on AMOUNT — tolerance applies only
// to DATES, where a settlement's bank credit may land a day or two later).
package reconcile

import (
	"sort"

	"github.com/razorpay/close-agent/internal/ingest"
	"github.com/razorpay/close-agent/internal/money"
)

// CheckNum identifies which of the SPEC §7 checks raised a break. It is a small
// int (1|2|3) so the investigate agent and the CLI can route on it.
type CheckNum int

const (
	// CheckSettlementBank is SPEC §7 check #1: every settlement's net_deposit
	// matches a bank-feed CREDIT of equal amount, matched by UTR/ref with the
	// value date within a small tolerance.
	CheckSettlementBank CheckNum = 1
	// CheckBatchSum is SPEC §7 check #2: for each settlement,
	// Σpayments − Σrefunds − Σfees == net_deposit.
	CheckBatchSum CheckNum = 2
	// CheckReceivableClears is SPEC §7 check #3: the settlement-receivable balance
	// at period end is ~0, allowing genuine T+2 in-transit settlements.
	CheckReceivableClears CheckNum = 3
)

// Break kinds — the stable vocabulary the investigate agent (Phase 8) and the
// learning seam route on. Each names the SHAPE of the failure, independent of
// the specific settlement.
const (
	// KindSettlementBankMismatch: a settlement has no matching bank credit, or the
	// matched credit's amount differs from net_deposit (check #1).
	KindSettlementBankMismatch = "settlement-bank-mismatch"
	// KindBatchSumMismatch: a settlement's batch members do not sum to its
	// net_deposit (check #2) — e.g. a refund omitted from the batch.
	KindBatchSumMismatch = "batch-sum-mismatch"
	// KindReceivableResidual: the receivable does not clear to ~0 at period end
	// beyond genuine in-transit amounts (check #3).
	KindReceivableResidual = "receivable-residual"
)

// Break is one reconciliation failure with enough context for the (Phase-8)
// investigate agent to form and test a hypothesis (SPEC §7). It is a plain data
// record — reconcile never resolves it.
//
//   - Check is which SPEC §7 check raised it (1|2|3).
//   - Kind is the stable failure shape (see the Kind* constants).
//   - SettlementID is the settlement the break concerns ("" for the period-wide
//     receivable-residual break, which is not tied to one settlement).
//   - Expected and Actual are the two money values the check compared, in paise:
//     for check #1 Expected is the settlement net_deposit and Actual is the
//     matched bank credit (zero if none matched); for check #2 Expected is the
//     batch-member sum and Actual is net_deposit; for check #3 Expected is ~0 and
//     Actual is the residual receivable.
//   - CandidateEventIDs are the related event ids the investigator should pull
//     (a settlement's batch members for check #2; the unmatched/late settlements
//     for check #3; the settlement id for check #1), in deterministic order.
//   - Detail is a short human-readable explanation for the CLI and the trace.
type Break struct {
	Check             CheckNum
	Kind              string
	SettlementID      string
	Expected          money.Money
	Actual            money.Money
	CandidateEventIDs []string
	Detail            string
}

// Input bundles everything Reconcile needs, all of it AGENT-INPUT or
// ledger-derived (never truth). Keeping it a struct makes the call site explicit
// about what reconciliation depends on and lets tests build a minimal scenario.
type Input struct {
	// Settlements are the raw settlement payouts for the period (the batch records).
	Settlements []ingest.RawSettlement
	// Payments / Refunds are the batch members, indexed by id inside Reconcile to
	// resolve each settlement's payment_ids / refund_ids for the batch-sum check.
	Payments []ingest.RawPayment
	Refunds  []ingest.RawRefund
	// BankFeed is the independent second record of cash movement (check #1).
	BankFeed ingest.RawBankFeed

	// ReceivableBalance is the settlement-receivable account balance at period end,
	// derived from the posted ledger by the caller (positive = still owed to us).
	// reconcile does not read the ledger itself, keeping it a pure value function.
	ReceivableBalance money.Money

	// PeriodEnd is the exclusive end of the period as a YYYY-MM-DD date string
	// (the 1st of the NEXT month). A settlement whose bank credit lands on or after
	// PeriodEnd is genuine T+2 in-transit and is excluded from the receivable
	// residual (check #3). Empty means "no period bound" — every settlement is
	// treated as in-period (used by unit scenarios that don't model T+2).
	PeriodEnd string

	// DateToleranceDays is the allowed |settlement date − bank credit date| in days
	// for a check #1 match (SPEC §7 "date within a small tolerance"). A settlement
	// posted on day D may credit the bank on D..D+tolerance. Zero means same-day.
	DateToleranceDays int
}

// Reconcile runs the SPEC §7 three checks over the input and returns every break
// it finds, in a deterministic order (by check number, then settlement id). An
// empty slice means the period reconciles fully (0 breaks) — the clean dtc
// period's expected result. Reconcile mutates nothing and reads nothing external.
func Reconcile(in Input) []Break {
	var breaks []Break

	breaks = append(breaks, checkSettlementBank(in)...)
	breaks = append(breaks, checkBatchSum(in)...)
	breaks = append(breaks, checkReceivableClears(in)...)

	// Stable order: check number first, then settlement id, so the break list is
	// byte-identical across runs regardless of the slice iteration above.
	sort.SliceStable(breaks, func(i, j int) bool {
		if breaks[i].Check != breaks[j].Check {
			return breaks[i].Check < breaks[j].Check
		}
		return breaks[i].SettlementID < breaks[j].SettlementID
	})
	return breaks
}

// indexPayments / indexRefunds build id -> record maps once so the batch-sum
// check can resolve each settlement's members in O(1). Maps are used only for
// LOOKUP (never iterated into output), so they introduce no nondeterminism.
func indexPayments(ps []ingest.RawPayment) map[string]ingest.RawPayment {
	m := make(map[string]ingest.RawPayment, len(ps))
	for _, p := range ps {
		m[p.ID] = p
	}
	return m
}

func indexRefunds(rs []ingest.RawRefund) map[string]ingest.RawRefund {
	m := make(map[string]ingest.RawRefund, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}
