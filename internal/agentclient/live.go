package agentclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/razorpay/close-agent/internal/money"
)

// live.go is the LIVE / RECORD transport (SPEC §8, §11 Phase 7, §12, §14): it
// posts an event to a configurable Flue HTTP endpoint (POST /agents/classify) and
// records the response into classify.recorded.json so a later CI run can REPLAY it
// deterministically. It is BUILT here so the seam is real and reviewable, but it
// is NOT exercised in CI — CI uses ReplayClient only (no live LLM, SPEC §12). The
// transport is kept behind the Client interface so Flue specifics never leak into
// the orchestrator (SPEC §14 "do not let Flue specifics leak into the Go
// orchestrator").

// classifyEndpoint is the §8 path on the Flue agent host the live client posts to.
const classifyEndpoint = "/agents/classify"

// defaultLiveTimeout bounds a single live classify call so a hung agent host does
// not stall an eval indefinitely. It is configurable via NewLiveClient's options
// in a future expansion; the value is fixed here for v1.
const defaultLiveTimeout = 30 * time.Second

// LiveClient posts to a Flue endpoint and optionally records each response into a
// recorded-response fixture for later replay. BaseURL is the agent host (e.g.
// http://localhost:8787); the client POSTs to BaseURL+classifyEndpoint. When
// recordPath is non-empty, each successful response is upserted into the fixture
// at that path so the next CI run can replay it.
//
// It satisfies Client. It does NOT read internal/truth; the agent host's tools are
// read-only Razorpay lookups (SPEC §8), and the recovery source is orders.json.
type LiveClient struct {
	baseURL    string
	httpClient *http.Client
	recordPath string       // empty = do not record; non-empty = record responses here
	recorded   RecordedFile // in-memory accumulator flushed by Flush
}

// NewLiveClient builds a LiveClient against baseURL. recordPath, if non-empty,
// names the classify.recorded.json the responses are recorded into (Flush writes
// it); world/period stamp the recorded file's header. A nil httpClient gets a
// default with defaultLiveTimeout.
func NewLiveClient(baseURL, world, period, recordPath string, httpClient *http.Client) *LiveClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultLiveTimeout}
	}
	return &LiveClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		recordPath: recordPath,
		recorded: RecordedFile{
			SchemaVersion: RecordedSchemaVersion,
			World:         world,
			Period:        period,
			Responses:     make([]RecordedResponse, 0),
		},
	}
}

// Mode reports live.
func (c *LiveClient) Mode() Mode { return ModeLive }

// classifyRequest is the §8 request body: { event: EventSummary }.
type classifyRequest struct {
	Event EventSummary `json:"event"`
}

// Classify posts ev to the Flue endpoint and returns the decoded ClassifyResult
// plus its FROZEN trace (SPEC §8). A transport/decode failure is an error (an
// infrastructure problem); an agent that declines is a normal {unclassifiable,
// reason} result, not an error. On a successful response, if recording is enabled,
// the response is folded into the in-memory recorded file (Flush persists it).
func (c *LiveClient) Classify(ev EventSummary) (ClassifyResult, Trace, error) {
	body, err := json.Marshal(classifyRequest{Event: ev})
	if err != nil {
		return ClassifyResult{}, Trace{}, fmt.Errorf("agentclient: marshal classify request: %w", err)
	}
	url := c.baseURL + classifyEndpoint
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return ClassifyResult{}, Trace{}, fmt.Errorf("agentclient: POST %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ClassifyResult{}, Trace{}, fmt.Errorf("agentclient: read response from %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return ClassifyResult{}, Trace{}, fmt.Errorf("agentclient: %s returned %d: %s", url, resp.StatusCode, string(data))
	}

	var out ClassifyResult
	if err := json.Unmarshal(data, &out); err != nil {
		return ClassifyResult{}, Trace{}, fmt.Errorf("agentclient: decode classify response: %w", err)
	}

	tr := newTrace(ModeLive, ev, []string{}, out)
	if c.recordPath != "" {
		c.recorded.upsert(recordedFromResult(ev.EventID, out))
	}
	return out, tr, nil
}

// Flush writes the accumulated recorded responses to recordPath (if recording is
// enabled), producing a committable classify.recorded.json for later replay. It is
// a no-op when recording is disabled.
func (c *LiveClient) Flush() error {
	if c.recordPath == "" {
		return nil
	}
	return WriteRecorded(c.recordPath, c.recorded)
}

// recordedFromResult projects a live ClassifyResult onto its on-disk RecordedResponse
// (money.Money params -> int64 paise). A classification records entry_type+params;
// an escalation records unclassifiable+reason — so replay reproduces either.
func recordedFromResult(eventID string, r ClassifyResult) RecordedResponse {
	if r.Unclassifiable {
		return RecordedResponse{EventID: eventID, Unclassifiable: true, Reason: r.Reason}
	}
	params := make(map[string]int64, len(r.Params))
	for k, v := range r.Params {
		params[k] = v.Paise()
	}
	return RecordedResponse{
		EventID:   eventID,
		EntryType: r.EntryType,
		Params:    params,
		Rationale: r.Rationale,
	}
}

// compile-time assertions that both transports satisfy Client.
var (
	_ Client = (*ReplayClient)(nil)
	_ Client = (*LiveClient)(nil)
)

// _ keeps money imported even if a future refactor drops its only use above.
var _ = money.FromPaise
