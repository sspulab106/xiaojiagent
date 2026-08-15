package provider

import "testing"

func TestV6Gateway(t *testing.T) {
	cases := []struct {
		subnet string
		want   string
	}{
		{"2001:db8::cafe:0/112", "2001:db8::cafe:1/112"},
		// Same address as the row above written differently; Go canonicalizes
		// the compression to the first longest zero-run.
		{"2001:db8:0:0:cafe::/112", "2001:db8::cafe:0:0:1/112"},
		{"fd00:10:91::/64", "fd00:10:91::1/64"},
		{"2a01:4f8:c17:cafe::/112", "2a01:4f8:c17:cafe::1/112"},
		// Host part is zeroed by the /112 mask, so gateway is the last block +1.
		{"2001:db8::ffff:ffff:ffff:ffff/112", "2001:db8::ffff:ffff:ffff:1/112"},
		// /128: the address itself is the network, +1 still applies.
		{"2001:db8::1/128", "2001:db8::2/128"},
	}
	for _, c := range cases {
		if got := v6Gateway(c.subnet); got != c.want {
			t.Errorf("v6Gateway(%q) = %q, want %q", c.subnet, got, c.want)
		}
	}
}

func TestV6GatewayRejectsInvalid(t *testing.T) {
	for _, s := range []string{"", "10.0.0.0/8", "2001:db8::/not-a-prefix", "garbage"} {
		if got := v6Gateway(s); got != "" {
			t.Errorf("v6Gateway(%q) = %q, want empty", s, got)
		}
	}
}
