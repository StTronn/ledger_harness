package classifyq

import "fmt"

// validate.go is the deterministic, automatic gate the APPLY stage runs on every
// proposed Result BEFORE any review or posting. It re-verifies the worker's
// citation against the snapshot and confirms the recovered rate is a real GST slab.
// This is what makes "the agent cannot inject a number" structural rather than
// trusted: a fabricated value would need a citation that, when re-read, doesn't
// match — and that mismatch is rejected here.

// gstSlabs is the closed set of valid GST rate percentages (mirrors
// internal/seed/gst.go gstRatePercents). A recovered rate outside this set is
// rejected — the worker can only land on a real slab.
var gstSlabs = map[int]bool{5: true, 12: true, 18: true}

// ValidateRate re-verifies a proposed Result's gst_rate citation against rates (the
// order_id -> gst_rate map read from orders.json, the same snapshot the worker
// read) and returns the validated integer rate. It errors if:
//
//   - the result carries no gst_rate citation;
//   - the cited source path is unsupported (v1 supports notes.gst_rate);
//   - the cited object is not in the snapshot;
//   - the cited value does NOT match what the snapshot actually holds (a forged or
//     stale citation);
//   - the value is not a valid GST slab.
//
// rates is re-read by the caller from orders.json at APPLY time, so this is a fresh,
// independent check of the worker's claim — never a trust of the stored value.
func ValidateRate(r Result, rates map[string]string) (int, error) {
	rec, ok := findRecovered(r, "gst_rate")
	if !ok {
		return 0, fmt.Errorf("result for %s has no gst_rate citation", r.EventID)
	}
	if rec.Source.Path != "notes.gst_rate" {
		return 0, fmt.Errorf("result for %s cites unsupported source path %q", r.EventID, rec.Source.Path)
	}
	actual, ok := rates[rec.Source.Object]
	if !ok {
		return 0, fmt.Errorf("result for %s cites order %q which is not in the snapshot", r.EventID, rec.Source.Object)
	}
	if actual != rec.Value {
		return 0, fmt.Errorf("citation mismatch for %s: claims gst_rate=%q but order %s holds %q",
			r.EventID, rec.Value, rec.Source.Object, actual)
	}
	n, ok := parseSlab(rec.Value)
	if !ok {
		return 0, fmt.Errorf("result for %s recovered gst_rate %q which is not a valid GST slab", r.EventID, rec.Value)
	}
	return n, nil
}

// findRecovered returns the recovered fact for the given field, if present.
func findRecovered(r Result, field string) (Recovered, bool) {
	for _, rec := range r.Recovered {
		if rec.Field == field {
			return rec, true
		}
	}
	return Recovered{}, false
}

// parseSlab parses a rate string to an int and checks it is a valid GST slab. It
// parses in integer space only (no float path), mirroring the rest of the system.
func parseSlab(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if !gstSlabs[n] {
		return 0, false
	}
	return n, true
}
