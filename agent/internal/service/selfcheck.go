package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// selfCheckMaxRuns bounds how many finished runs are kept in memory.
	selfCheckMaxRuns = 20
	// selfCheckTimeout bounds one run (image pulls + echo probes can take a
	// while; the script itself has its own per-step timeouts).
	selfCheckTimeout = 15 * time.Minute
)

// SelfCheckRun is a snapshot of one verify-ndp.sh execution. Status is
// "running", "done" (exit 0) or "failed" (nonzero exit / spawn error).
type SelfCheckRun struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Output     string    `json:"output"`
	ExitCode   int       `json:"exit_code"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// SelfCheckManager runs scripts/verify-ndp.sh asynchronously so the master's
// admin panel can trigger a node self-check and poll the output without
// blocking an HTTP request for minutes.
type SelfCheckManager struct {
	mu      sync.Mutex
	runs    map[string]*SelfCheckRun
	order   []string // insertion order, for pruning oldest finished runs
	script  string   // path to verify-ndp.sh
	dataDir string   // agent data dir (config.json + verify-ndp.sh live here)
	baseURL string   // this agent's local URL, passed to the script
	log     *slog.Logger
}

// NewSelfCheckManager builds the manager. script may be empty (resolved by the
// caller); Start then returns a clear error.
func NewSelfCheckManager(script, dataDir, baseURL string, log *slog.Logger) *SelfCheckManager {
	return &SelfCheckManager{
		runs:    make(map[string]*SelfCheckRun),
		script:  script,
		dataDir: dataDir,
		baseURL: baseURL,
		log:     log,
	}
}

// Start launches verify-ndp.sh in the background and returns the run id.
// It fails fast when the script is missing (e.g. the node was installed
// before the self-check feature shipped).
func (m *SelfCheckManager) Start() (string, error) {
	if m.script == "" || !fileExists(m.script) {
		return "", errors.New("节点缺少 verify-ndp.sh（" + m.script + "）。可重新生成安装脚本装一次，或手动把 scripts/verify-ndp.sh 放到该路径")
	}
	run := &SelfCheckRun{ID: randHex(8), Status: "running", StartedAt: time.Now()}
	m.mu.Lock()
	m.runs[run.ID] = run
	m.order = append(m.order, run.ID)
	m.pruneLocked()
	m.mu.Unlock()
	go m.exec(run)
	return run.ID, nil
}

// Status returns a copy of the run snapshot, or ok=false when the id is
// unknown or already pruned.
func (m *SelfCheckManager) Status(id string) (*SelfCheckRun, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// pruneLocked drops finished runs beyond the cap. A run that is still
// "running" is never evicted — the master polls it and must never hit a 404
// mid-flight just because 20+ concurrent runs accumulated. Called with m.mu
// held.
func (m *SelfCheckManager) pruneLocked() {
	for len(m.order) > selfCheckMaxRuns {
		idx := -1
		for i, id := range m.order {
			if m.runs[id].Status != "running" {
				idx = i
				break
			}
		}
		if idx < 0 {
			return // all runs still running; keep them all
		}
		old := m.order[idx]
		m.order = append(m.order[:idx], m.order[idx+1:]...)
		delete(m.runs, old)
	}
}

// exec runs one self-check. The script reads the agent token from its own
// config.json (--data-dir) and targets this agent's local URL.
func (m *SelfCheckManager) exec(run *SelfCheckRun) {
	ctx, cancel := context.WithTimeout(context.Background(), selfCheckTimeout)
	defer cancel()
	cmd := exec.Command("bash", m.script, "--data-dir", m.dataDir, m.baseURL)
	// Own process group: on timeout we kill the whole group (bash + any curl /
	// docker exec children) instead of orphaning them. Note the killed script
	// cannot run its cleanup, so a temp instance may remain — the timeout note
	// in the output says so, and the script is designed to be re-run safely.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		m.finish(run, &buf, err, false)
		return
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		m.finish(run, &buf, err, false)
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // kill whole group
		<-done
		m.finish(run, &buf, ctx.Err(), true)
	}
}

// finish records the terminal state of a run under the manager lock.
func (m *SelfCheckManager) finish(run *SelfCheckRun, buf *bytes.Buffer, err error, timedOut bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run.Output = buf.String()
	run.FinishedAt = time.Now()
	if err == nil {
		run.Status = "done"
		run.ExitCode = 0
		return
	}
	run.Status = "failed"
	if timedOut {
		run.Output += "\n[timeout] 自检运行超过 " + selfCheckTimeout.String() + " 已被终止（脚本未及清理，可能残留临时实例，重跑自检或手动清理）"
		run.ExitCode = -1
		return
	}
	if ee, ok := err.(*exec.ExitError); ok {
		run.ExitCode = ee.ExitCode()
	} else {
		run.ExitCode = -1
	}
	if m.log != nil {
		m.log.Warn("self-check finished with error", "id", run.ID, "err", err)
	}
}

// localBaseURL derives http://127.0.0.1:<port> from a Listen value like
// ":8792" or "0.0.0.0:8792".
func localBaseURL(listen string) string {
	port := strings.TrimPrefix(listen, ":")
	if i := strings.LastIndexByte(port, ':'); i >= 0 {
		port = port[i+1:]
	}
	return "http://127.0.0.1:" + port
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}
