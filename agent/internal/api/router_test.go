package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/codetest/agent/internal/config"
	"example.com/codetest/agent/internal/service"
)

func newTestAgent(t *testing.T, script string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "verify-ndp.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Listen:          ":8792",
		Token:           "tok123",
		DataDir:         dir,
		VirtType:        "oci",
		VerifyNdpScript: filepath.Join(dir, "verify-ndp.sh"),
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := service.New(cfg, nil, logger)
	r := New(cfg, svc)
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts
}

func TestSelfCheckEndpoints(t *testing.T) {
	script := "#!/bin/bash\necho start\necho step-ok\nexit 0\n"
	ts := newTestAgent(t, script)

	// 未带 Token -> 401
	resp, err := http.Post(ts.URL+"/agent/v1/selfcheck", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", resp.StatusCode)
	}

	// 带 Token 触发自检
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/agent/v1/selfcheck", nil)
	req.Header.Set("Authorization", "Bearer tok123")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var start struct {
		Code int `json:"code"`
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if start.Data.ID == "" || start.Data.Status != "running" {
		t.Fatalf("start reply = %+v, want id + running", start)
	}

	// 轮询状态直到完成
	deadline := time.Now().Add(10 * time.Second)
	for {
		req, _ = http.NewRequest(http.MethodGet, ts.URL+"/agent/v1/selfcheck/"+start.Data.ID, nil)
		req.Header.Set("Authorization", "Bearer tok123")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var st struct {
			Code int `json:"code"`
			Data struct {
				ID       string `json:"id"`
				Status   string `json:"status"`
				Output   string `json:"output"`
				ExitCode int    `json:"exit_code"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if st.Data.Status != "running" {
			if st.Data.Status != "done" || st.Data.ExitCode != 0 || !strings.Contains(st.Data.Output, "step-ok") {
				t.Fatalf("final state = %+v", st.Data)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("self-check still running after timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 未知 run id -> 404
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/agent/v1/selfcheck/nope", nil)
	req.Header.Set("Authorization", "Bearer tok123")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want 404", resp.StatusCode)
	}
}

func TestSelfCheckEndpointsMissingScript(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Listen:          ":8792",
		Token:           "tok123",
		DataDir:         dir,
		VerifyNdpScript: filepath.Join(dir, "verify-ndp.sh"), // does not exist
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := service.New(cfg, nil, logger)
	ts := httptest.NewServer(New(cfg, svc))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/agent/v1/selfcheck", nil)
	req.Header.Set("Authorization", "Bearer tok123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing-script status = %d, want 400", resp.StatusCode)
	}
}
