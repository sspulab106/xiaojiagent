package ndp

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func quiet() *slog.Logger {
	// io.Discard keeps this usable on the module's declared Go 1.22 minimum
	// (slog.DiscardHandler only exists from Go 1.24).
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestParseSubnets(t *testing.T) {
	m := New("eth0", "2001:db8::cafe:0/112, 2001:db8::d00d:0/112, garbage", quiet())
	if !m.Enabled() {
		t.Fatal("expected enabled manager")
	}
	if m.Iface() != "eth0" {
		t.Fatalf("iface = %q", m.Iface())
	}
	subs := m.Subnets()
	if len(subs) != 2 {
		t.Fatalf("subnets = %v, want 2", subs)
	}
}

func TestDisabledWithoutIfaceOrSubnets(t *testing.T) {
	if New("", "2001:db8::/64", quiet()).Enabled() {
		t.Fatal("empty iface must disable manager")
	}
	if New("eth0", "", quiet()).Enabled() {
		t.Fatal("no subnets must disable manager")
	}
	if New("eth0", "not-a-cidr", quiet()).Enabled() {
		t.Fatal("only invalid subnets must disable manager")
	}
}

func TestAddRejectsOutOfScope(t *testing.T) {
	m := New("eth0", "2001:db8::cafe:0/112", quiet())
	// In-subnet but still rejected before any exec when IPv4 or malformed.
	for _, addr := range []string{"", "nope", "192.168.1.5", "2001:db8::d00d:1"} {
		if err := m.Add(addr); err == nil {
			t.Fatalf("Add(%q) should fail before touching the kernel", addr)
		}
	}
}

func TestSyncIgnoresOutOfScope(t *testing.T) {
	m := New("eth0", "2001:db8::cafe:0/112", quiet())
	// Sync with mixed addresses: out-of-scope ones are skipped without exec,
	// so the proxied table stays empty.
	n := m.Sync([]string{"2001:db8::d00d:1", "fe80::1", "192.168.0.2"})
	if n != 0 {
		t.Fatalf("expected 0 proxied, got %d", n)
	}
	if len(m.Proxied()) != 0 {
		t.Fatalf("proxied = %v, want empty", m.Proxied())
	}
}

func TestInfoDisabled(t *testing.T) {
	m := New("", "", quiet())
	info := m.Info()
	if info["enabled"] != false {
		t.Fatalf("disabled manager should report enabled=false, got %v", info)
	}
	if len(m.Proxied()) != 0 {
		t.Fatalf("Proxied() on disabled manager should be empty, got %v", m.Proxied())
	}
}

func TestSubnetsListKeepsConfigOrder(t *testing.T) {
	m := New("eth0", "2001:db8::b:0/112,2001:db8::a:0/112", quiet())
	subs := strings.Join(m.Subnets(), ",")
	if subs != "2001:db8::b:0/112,2001:db8::a:0/112" {
		t.Fatalf("subnets = %q", subs)
	}
}
