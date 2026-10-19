package main

import "testing"

func TestExtractDirectPathPreservesAuthorization(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"https://mmg.whatsapp.net/v/t62/file.enc?ccb=11-4&oh=signature&oe=expiry", "/v/t62/file.enc?ccb=11-4&oh=signature&oe=expiry"},
		{"https://media.example.com/o1/v/t24/file.enc?token=a%2Fb%3D&x=1#fragment", "/o1/v/t24/file.enc?token=a%2Fb%3D&x=1"},
		{"/v/t62/file.enc?oh=signature", "/v/t62/file.enc?oh=signature"},
		{"https://mmg.whatsapp.net/file.enc", "/file.enc"},
		{"http://mmg.whatsapp.net/file.enc", ""},
		{"not-a-path", ""},
		{"https://%invalid", ""},
	} {
		if got := extractDirectPathFromURL(tc.input); got != tc.want {
			t.Errorf("extractDirectPathFromURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
