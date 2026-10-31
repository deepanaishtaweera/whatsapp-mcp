package main

import "testing"

func TestParseStatusBackground(t *testing.T) {
	cases := []struct {
		color   string
		want    uint32
		wantErr bool
	}{
		{"", defaultStatusBackground, false},
		{"#FF0000", 0xFFFF0000, false},
		{"00ff7f", 0xFF00FF7F, false},
		{"#FFF", 0, true},
		{"#GG0000", 0, true},
		{"#FF000000", 0, true},
	}

	for _, c := range cases {
		got, err := parseStatusBackground(c.color)
		if (err != nil) != c.wantErr {
			t.Errorf("parseStatusBackground(%q) error = %v, wantErr %v", c.color, err, c.wantErr)
			continue
		}
		if got != c.want {
			t.Errorf("parseStatusBackground(%q) = %#x, want %#x", c.color, got, c.want)
		}
	}
}
