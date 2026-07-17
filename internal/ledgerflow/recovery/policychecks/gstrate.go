package policychecks

// gst-rate-from-order: the original recovery (SPEC §1, §2). When a payment or
// refund arrives without its gst_rate, the PARENT ORDER is the authority — it
// keeps the true rate even after the event's own copy was stripped. Walk:
// payment -> order, or refund -> payment -> order. Validation: the closed GST
// slab set — a recovered "rate" outside it is surfaced as invalid, never used.

// gstSlabs is the closed set of GST rates the catalogue uses. A recovery
// producing anything else is marked invalid (visible, not trusted).
var gstSlabs = map[string]bool{"5": true, "12": true, "18": true}

type gstRateFromOrder struct{}

func (gstRateFromOrder) Name() string { return "gst-rate-from-order" }

func (gstRateFromOrder) AppliesTo(ev Event) bool {
	return (ev.Type == "payment" || ev.Type == "refund") && ev.GSTRate == ""
}

func (gstRateFromOrder) Recover(ev Event, g Graph) Finding {
	var orderID string
	switch ev.Type {
	case "payment":
		orderID, _ = g.OrderIDForPayment(ev.EventID)
	case "refund":
		if payID, ok := g.PaymentIDForRefund(ev.EventID); ok {
			orderID, _ = g.OrderIDForPayment(payID)
		}
	}
	if orderID == "" {
		return Finding{}
	}
	o, ok := g.OrderInfo(orderID)
	if !ok || o.GSTRate == "" {
		return Finding{}
	}
	return Finding{Facts: []Fact{{
		Field:  "gst_rate",
		Value:  o.GSTRate,
		Source: Citation{Object: orderID, Path: "notes.gst_rate"},
		Valid:  gstSlabs[o.GSTRate],
		Policy: gstRateFromOrder{}.Name(),
	}}}
}
