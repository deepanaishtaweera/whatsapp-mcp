package main

import "testing"

func TestAPIHost(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"", defaultAPIHost},
		{"100.64.0.1", "100.64.0.1"},
		{"0.0.0.0", "0.0.0.0"},
		{"::1", "::1"},
	}

	for _, c := range cases {
		t.Setenv("WHATSAPP_API_HOST", c.env)
		if got := apiHost(); got != c.want {
			t.Errorf("apiHost() with WHATSAPP_API_HOST=%q = %q, want %q", c.env, got, c.want)
		}
	}
}
