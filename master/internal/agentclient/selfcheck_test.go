package agentclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockAgent serves the self-check endpoints with the shared {code,message,data}
// envelope, mirroring the agent.
func mockAgent(t *testing.T, runID string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/v1/selfcheck", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "message": "ok",
			"data": map[string]any{"id": runID, "status": "running"},
		})
	})
	mux.HandleFunc("/agent/v1/selfcheck/"+runID, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "message": "ok",
			"data": map[string]any{
				"id": runID, "status": "failed", "exit_code": 3,
				"output": "[FAIL] x\n", "started_at": "2026-01-01T00:00:00Z",
				"finished_at": "2026-01-01T00:00:01Z",
			},
		})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestSelfCheckRoundTrip(t *testing.T) {
	ts := mockAgent(t, "run-abc")
	c := New(ts.URL, "tok")

	st, err := c.StartSelfCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.ID != "run-abc" || st.Status != "running" {
		t.Fatalf("start = %+v", st)
	}

	run, err := c.SelfCheckStatus(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || run.ExitCode != 3 || run.Output != "[FAIL] x\n" {
		t.Fatalf("status = %+v", run)
	}
	if run.StartedAt.IsZero() || run.FinishedAt.IsZero() {
		t.Fatalf("timestamps not parsed: %+v", run)
	}
}
