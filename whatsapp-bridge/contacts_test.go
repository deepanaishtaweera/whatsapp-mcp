package main

import "testing"

func TestNormalizePhone(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"+94771234567", "94771234567"},
		{"94771234567", "94771234567"},
		{"+94 77-123 4567", "94771234567"},
		{"+1 (415) 555.0100", "14155550100"},
		{"0094771234567", "94771234567"},
		{" +447700900123 ", "447700900123"},
	} {
		got, err := normalizePhone(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("normalizePhone(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}

	for _, input := range []string{"", "0771234567", "+94abc1234567", "12345", "+1234567890123456", "94771234567@s.whatsapp.net"} {
		if got, err := normalizePhone(input); err == nil {
			t.Errorf("normalizePhone(%q) = %q, want error", input, got)
		}
	}
}
