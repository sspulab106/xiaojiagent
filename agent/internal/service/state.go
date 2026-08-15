package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"example.com/codetest/agent/internal/nat"
	"example.com/codetest/agent/internal/provider"
)

// State is the agent's persisted view: the NAT rules it manages and the specs
// of instances it created (needed to rebuild with the same limits). Stored as
// a single JSON file in the data dir.
type State struct {
	mu        sync.Mutex
	path      string
	Rules     []nat.Rule               `json:"rules"`
	Instances map[string]provider.Spec `json:"instances"`
}

func loadState(dataDir string) *State {
	path := filepath.Join(dataDir, "state.json")
	st := &State{path: path, Instances: make(map[string]provider.Spec)}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, st)
	}
	if st.Instances == nil {
		st.Instances = make(map[string]provider.Spec)
	}
	return st
}

func (s *State) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

func (s *State) SetInstance(spec provider.Spec) {
	s.mu.Lock()
	s.Instances[spec.Name] = spec
	s.mu.Unlock()
	_ = s.save()
}

func (s *State) GetInstance(name string) (provider.Spec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spec, ok := s.Instances[name]
	return spec, ok
}

func (s *State) RemoveInstance(name string) {
	s.mu.Lock()
	delete(s.Instances, name)
	s.mu.Unlock()
	_ = s.save()
}

func (s *State) AddRule(r nat.Rule) {
	s.mu.Lock()
	s.Rules = append(s.Rules, r)
	s.mu.Unlock()
	_ = s.save()
}

// RulesCopy returns a snapshot of the persisted rules.
func (s *State) RulesCopy() []nat.Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]nat.Rule, len(s.Rules))
	copy(out, s.Rules)
	return out
}

// UpdateRule replaces a persisted rule in place (same ID, possibly a new
// container IP).
func (s *State) UpdateRule(r nat.Rule) {
	s.mu.Lock()
	for i, cur := range s.Rules {
		if cur.ID == r.ID {
			s.Rules[i] = r
			break
		}
	}
	s.mu.Unlock()
	_ = s.save()
}

func (s *State) RemoveRule(id string) {
	s.mu.Lock()
	for i, r := range s.Rules {
		if r.ID == id {
			s.Rules = append(s.Rules[:i], s.Rules[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	_ = s.save()
}
