package seed

import (
	"fmt"

	"github.com/razorpay/ledger-flow/internal/money"
)

// cod.go generates the CASH-ON-DELIVERY rail (ROADMAP §8.3): a second money rail
// where the courier — not Razorpay — collects cash at the door and remits it in a
// netted weekly batch. With Options.COD on, the generator appends one COD world
// to the month, ALONGSIDE the Razorpay batches (the two rails are independent;
// each receivable nets to ~0 on its own), so a COD period proves the engine
// closes a second rail without disturbing the first.
//
// The world is the RTO judgment scenario by construction:
//
//	N delivered shipments  -> N cod_sale entries (revenue at delivery)
//	1 RTO shipment         -> no sale (never delivered), but the courier charges a
//	                          reverse-logistics fee, netted out of the remittance
//	1 remittance           -> cod_remittance books ONLY the collection-fee portion
//
// The courier also nets out two per-shipment deductions the deterministic rules
// cannot book — the RTO fee (rate-card-backed) and a weight-dispute adjustment
// (no rate-card basis) — so the COD receivable is left short by exactly their sum
// (₹118 + ₹40 = ₹158). Truth books all four entry types, so truth's receivable
// nets to 0; the rule-only close is short by the rto_fee + weight_adjustment,
// which is the residual the investigate agent decomposes (books the RTO fee,
// escalates the weight adjustment).
//
// Money is integer paise throughout; the netting identities below hold to the
// paise, which is what keeps the truth GL balanced and the residual exact.

const (
	numCODDeliveries      = 6 // delivered COD shipments in the month
	codCourierChannel     = "delhivery"
	codCollectionFeePaise = 5000  // ₹50.00 flat COD collection fee per shipment
	rtoFeeNetPaise        = 10000 // ₹100.00 RTO fee (GST-exclusive); +18% => ₹118 gross
	weightAdjPaise        = 4000  // ₹40.00 weight-dispute deduction (the escalation)
	codShipLeadDays       = 3     // a parcel ships this many days before it resolves
	weightAdjDeliveryIdx  = 2     // which delivered shipment (0-based) bears the weight dispute
)

// generateCOD appends the COD rail to the month. It draws delivery grosses from
// the SAME seeded RNG (after the Razorpay batches), so a COD period is still
// fully deterministic; periods without Options.COD never call it, so the RNG
// stream — and every Razorpay-only period — is unchanged. It returns an error
// only on a truth-GL bind/post failure (a generation bug).
func (g *generator) generateCOD() error {
	deliverDayOffset := g.cal.daysInMon / 2
	remitDayOffset := deliverDayOffset + 7
	if remitDayOffset > g.cal.daysInMon-1 {
		remitDayOffset = g.cal.daysInMon - 1
	}
	shipDayOffset := deliverDayOffset - codShipLeadDays
	if shipDayOffset < 0 {
		shipDayOffset = 0
	}
	shipTs := g.cal.epochForDayOffset(shipDayOffset)
	deliverTs := g.cal.epochForDayOffset(deliverDayOffset)
	remitTs := g.cal.epochForDayOffset(remitDayOffset)

	shipments := make([]CODShipment, 0, numCODDeliveries+1)
	var grossCollected money.Money
	var weightAdjShipment string

	for k := 0; k < numCODDeliveries; k++ {
		gross := money.FromPaise(g.rng.Int64Range(grossLoPaise, grossHiPaise))
		rate := gstRatePercents[g.rng.IntRange(0, len(gstRatePercents)-1)]
		sku := g.rng.Pick(skuOptions)
		shp := g.ids.shipment()
		ord := g.ids.order()

		shipments = append(shipments, CODShipment{
			Entity:     "shipment",
			ID:         shp,
			OrderID:    ord,
			CODAmount:  gross,
			Status:     "delivered",
			ShippedAt:  shipTs,
			ResolvedAt: deliverTs,
			Notes:      Notes{SKU: sku, GSTRate: fmt.Sprintf("%d", rate)},
		})
		grossCollected = grossCollected.Add(gross)

		net, gst := splitGSTInclusive(gross, rate)
		if err := g.addCODSaleEntry(shp, gross, net, gst, deliverTs); err != nil {
			return err
		}
		if k == weightAdjDeliveryIdx {
			weightAdjShipment = shp
		}
	}

	// The RTO parcel: shipped, refused, returned to origin — it collected no cash
	// (revenue-at-delivery means no cod_sale ever existed for it), but the courier
	// charges a reverse-logistics fee for the return leg.
	rtoShp := g.ids.shipment()
	rtoOrd := g.ids.order()
	rtoSku := g.rng.Pick(skuOptions)
	shipments = append(shipments, CODShipment{
		Entity:     "shipment",
		ID:         rtoShp,
		OrderID:    rtoOrd,
		CODAmount:  money.FromPaise(0),
		Status:     "rto",
		ShippedAt:  shipTs,
		ResolvedAt: deliverTs,
		Notes:      Notes{SKU: rtoSku, GSTRate: fmt.Sprintf("%d", razorpayGSTRate)},
	})

	// Deductions the courier nets out of the remittance, beyond its collection fee.
	rtoNet := money.FromPaise(rtoFeeNetPaise)
	rtoGST := gstOnFee(rtoNet, razorpayGSTRate)
	rtoGross := rtoNet.Add(rtoGST)
	weightAdj := money.FromPaise(weightAdjPaise)

	// Collection fee + GST across the delivered shipments (per-shipment, summed).
	collectionFee := money.FromPaise(codCollectionFeePaise * numCODDeliveries)
	gstPerShipment := gstOnFee(money.FromPaise(codCollectionFeePaise), razorpayGSTRate)
	gstOnCollection := money.FromPaise(gstPerShipment.Paise() * numCODDeliveries)

	// The courier wires what it collected, less its fee+GST and the deductions.
	netDeposit := grossCollected.
		Sub(collectionFee).Sub(gstOnCollection).
		Sub(rtoGross).Sub(weightAdj)

	// The cod_remittance entry clears only the collection-fee portion of the gross
	// (net_deposit + fee + gst_on_fee == grossCollected − deductions). The
	// deductions stay on the receivable until the rto_fee / weight_adjustment book.
	remitGross := netDeposit.Add(collectionFee).Add(gstOnCollection)

	rem := g.ids.remittance()
	utr := utrForRemittance(rem)

	// Each deduction gets a unique id, used as the event id of the entry that
	// books it. A delivered shipment's cod_sale already keys off the shipment id,
	// so a deduction on a delivered shipment (the weight dispute on shp_03) must
	// NOT reuse the shipment id — that would collide in the scorer's event-keyed
	// match. The deduction id is distinct, so rto_fee and weight_adjustment each
	// score independently.
	rtoDedID := g.ids.deduction()
	weightDedID := g.ids.deduction()

	if err := g.addCODRemittanceEntry(rem, utr, netDeposit, collectionFee, gstOnCollection, remitGross, remitTs); err != nil {
		return err
	}
	// Truth books the two deductions too, so truth's receivable nets to 0. The
	// rule-only close cannot (no per-event rule fits a deduction line), leaving the
	// receivable short by exactly rtoGross + weightAdj — the investigate residual.
	if err := g.addRTOFeeEntry(rtoDedID, rtoNet, rtoGST, remitTs); err != nil {
		return err
	}
	if err := g.addWeightAdjustmentEntry(weightDedID, weightAdj, remitTs); err != nil {
		return err
	}

	g.bankCredits = append(g.bankCredits, BankFeedEntry{
		Amount: netDeposit,
		Date:   dateString(remitTs),
		Ref:    utr,
	})

	g.courier = &CourierFeed{
		Channel:   codCourierChannel,
		Period:    g.period,
		Shipments: shipments,
		Remittances: []CODRemittance{{
			Entity:         "remittance",
			ID:             rem,
			GrossCollected: grossCollected,
			CollectionFee:  collectionFee,
			GSTOnFee:       gstOnCollection,
			NetDeposit:     netDeposit,
			UTR:            utr,
			CreatedAt:      remitTs,
			Deductions: []CODDeduction{
				{ID: rtoDedID, Code: "RTO_CHG", ShipmentID: rtoShp, Amount: rtoGross},
				{ID: weightDedID, Code: "WT_ADJ", ShipmentID: weightAdjShipment, Amount: weightAdj},
			},
		}},
	}
	return nil
}

// utrForRemittance derives a deterministic bank UTR from a remittance id, reusing
// its token so the remittance and its bank-feed credit share a stable reference.
func utrForRemittance(remittanceID string) string {
	return "UTR" + remittanceID[len(prefixRemittance):]
}

// addCODSaleEntry binds and posts the cod_sale truth-GL entry for a delivered
// shipment:
//
//	Dr assets/cod-receivable          {gross}
//	Cr income/product-sales           {net}
//	Cr liabilities/gst-output-payable {gst}
func (g *generator) addCODSaleEntry(shipmentID string, gross, net, gst money.Money, ts int64) error {
	params := map[string]money.Money{
		"gross":       gross,
		"net":         net,
		"gst":         gst,
		"shipment_id": txIDParam(),
	}
	return g.binder.bind("cod_sale", "codsale:"+shipmentID, shipmentID, shipmentID, ts, params)
}

// addCODRemittanceEntry binds and posts the cod_remittance truth-GL entry:
//
//	Dr assets/bank                {net_deposit}
//	Dr expense/cod-collection-fees {fee}
//	Dr expense/gst-input          {gst_on_fee}
//	Cr assets/cod-receivable      {gross}
//
// net_deposit + fee + gst_on_fee == gross by construction.
func (g *generator) addCODRemittanceEntry(remID, utr string, netDeposit, fee, gstOnFee, gross money.Money, ts int64) error {
	params := map[string]money.Money{
		"net_deposit":   netDeposit,
		"fee":           fee,
		"gst_on_fee":    gstOnFee,
		"gross":         gross,
		"remittance_id": txIDParam(),
	}
	return g.binder.bind("cod_remittance", "codremit:"+remID, remID, utr, ts, params)
}

// addRTOFeeEntry binds and posts the rto_fee truth-GL entry for the RTO charge:
//
//	Dr expense/reverse-logistics {net}
//	Dr expense/gst-input         {gst}
//	Cr assets/cod-receivable     {net+gst}
//
// The event id is the DEDUCTION id (not the shipment), so it never collides with
// a cod_sale and the investigate agent's posting (keyed to the same deduction)
// matches truth in the scorer.
func (g *generator) addRTOFeeEntry(deductionID string, net, gst money.Money, ts int64) error {
	params := map[string]money.Money{
		"net":         net,
		"gst":         gst,
		"shipment_id": txIDParam(),
	}
	return g.binder.bind("rto_fee", "rtofee:"+deductionID, deductionID, deductionID, ts, params)
}

// addWeightAdjustmentEntry binds and posts the weight_adjustment truth-GL entry:
//
//	Dr expense/reverse-logistics {amount}
//	Cr assets/cod-receivable     {amount}
//
// Truth books it (so the receivable nets to 0), but the agent escalates it — an
// unverified deduction with no rate-card basis — so the rule+agent close stays
// honestly short by this one entry. Keyed to the deduction id (see addRTOFeeEntry).
func (g *generator) addWeightAdjustmentEntry(deductionID string, amount money.Money, ts int64) error {
	params := map[string]money.Money{
		"amount":      amount,
		"shipment_id": txIDParam(),
	}
	return g.binder.bind("weight_adjustment", "wtadj:"+deductionID, deductionID, deductionID, ts, params)
}
