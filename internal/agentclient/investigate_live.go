package agentclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// investigate_live.go is the LIVE / RECORD transport for the §8 investigate seam
// (SPEC §8, §11 Phase 8, §12, §14), parallel to live.go for classify: it posts a
// break (+ its candidate events) to a configurable Flue HTTP endpoint
// (POST /agents/investigate) and records the response into
// investigate.recorded.json so a later CI run can REPLAY it deterministically. It
// is BUILT here so the seam is real and reviewable, but NOT exercised in CI — the
// Flue investigate endpoint lands in Phase 7b.

// investigateEndpoint is the §8 path on the Flue agent host the live client posts to.
const investigateEndpoint = "/agents/investigate"

// LiveInvestigateClient posts to a Flue endpoint and optionally records each
// response into a recorded-investigation fixture for later replay. It satisfies
// InvestigateClient and does NOT read internal/truth.
type LiveInvestigateClient struct {
	baseURL    string
	httpClient *http.Client
	recordPath string                  // empty = do not record
	recorded   RecordedInvestigateFile // in-memory accumulator flushed by Flush
}

// NewLiveInvestigateClient builds a LiveInvestigateClient against baseURL.
// recordPath, if non-empty, names the investigate.recorded.json the responses are
// recorded into (Flush writes it); world/period stamp the recorded file's header.
// A nil httpClient gets a default with defaultLiveTimeout (defined in live.go).
func NewLiveInvestigateClient(baseURL, world, period, recordPath string, httpClient *http.Client) *LiveInvestigateClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultLiveTimeout}
	}
	return &LiveInvestigateClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		recordPath: recordPath,
		recorded: RecordedInvestigateFile{
			SchemaVersion: InvestigateRecordedSchemaVersion,
			World:         world,
			Period:        period,
			Resolutions:   make([]RecordedResolution, 0),
		},
	}
}

// Mode reports live.
func (c *LiveInvestigateClient) Mode() Mode { return ModeLive }

// investigateRequest is the §8 request body: { break: BreakSummary, candidates: EventSummary[] }.
type investigateRequest struct {
	Break      BreakSummary   `json:"break"`
	Candidates []EventSummary `json:"candidates"`
}

// Investigate posts the break (+ candidates) to the Flue endpoint and returns the
// decoded InvestigateResult plus its FROZEN trace (SPEC §8). A transport/decode
// failure is an error; an agent that escalates is a normal {escalate, reason}
// result, not an error. On a successful response, if recording is enabled, the
// response is folded into the in-memory recorded file (Flush persists it).
func (c *LiveInvestigateClient) Investigate(brk BreakSummary, candidates []EventSummary) (InvestigateResult, InvestigateTrace, error) {
	body, err := json.Marshal(investigateRequest{Break: brk, Candidates: candidates})
	if err != nil {
		return InvestigateResult{}, InvestigateTrace{}, fmt.Errorf("agentclient: marshal investigate request: %w", err)
	}
	url := c.baseURL + investigateEndpoint
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return InvestigateResult{}, InvestigateTrace{}, fmt.Errorf("agentclient: POST %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return InvestigateResult{}, InvestigateTrace{}, fmt.Errorf("agentclient: read response from %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return InvestigateResult{}, InvestigateTrace{}, fmt.Errorf("agentclient: %s returned %d: %s", url, resp.StatusCode, string(data))
	}

	var out InvestigateResult
	if err := json.Unmarshal(data, &out); err != nil {
		return InvestigateResult{}, InvestigateTrace{}, fmt.Errorf("agentclient: decode investigate response: %w", err)
	}

	tr := newInvestigateTrace(ModeLive, brk, candidates, []string{}, out)
	if c.recordPath != "" {
		c.recorded.upsert(recordedFromInvestigation(brk.Key, out))
	}
	return out, tr, nil
}

// Flush writes the accumulated recorded investigations to recordPath (if recording
// is enabled), producing a committable investigate.recorded.json for later replay.
// It is a no-op when recording is disabled.
func (c *LiveInvestigateClient) Flush() error {
	if c.recordPath == "" {
		return nil
	}
	return WriteInvestigateRecorded(c.recordPath, c.recorded)
}

// recordedFromInvestigation projects a live InvestigateResult onto its on-disk
// RecordedResolution (money.Money params -> int64 paise). A resolution records its
// postings; an escalation records escalate+reason — so replay reproduces either.
func recordedFromInvestigation(breakKey string, r InvestigateResult) RecordedResolution {
	if !r.Resolvable() {
		return RecordedResolution{BreakKey: breakKey, Escalate: true, Reason: r.Reason}
	}
	postings := make([]RecordedPosting, 0, len(r.Resolution))
	for _, p := range r.Resolution {
		params := make(map[string]int64, len(p.Params))
		for k, v := range p.Params {
			params[k] = v.Paise()
		}
		postings = append(postings, RecordedPosting{EventID: p.EventID, EntryType: p.EntryType, Params: params})
	}
	return RecordedResolution{BreakKey: breakKey, Resolution: postings, Rationale: r.Rationale}
}

// compile-time assertions that both transports satisfy InvestigateClient.
var (
	_ InvestigateClient = (*ReplayInvestigateClient)(nil)
	_ InvestigateClient = (*LiveInvestigateClient)(nil)
)
