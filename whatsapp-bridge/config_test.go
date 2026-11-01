package main

import "testing"

func TestAPIAddr(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"", "127.0.0.1:8080"},
		{"100.64.0.1", "100.64.0.1:8080"},
		{" 0.0.0.0 ", "0.0.0.0:8080"},
		{"::1", "[::1]:8080"},
	}

	for _, c := range cases {
		t.Setenv("WHATSAPP_API_HOST", c.env)
		if got := apiAddr(8080); got != c.want {
			t.Errorf("apiAddr(8080) with WHATSAPP_API_HOST=%q = %q, want %q", c.env, got, c.want)
		}
	}
}
