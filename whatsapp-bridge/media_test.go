package main

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	waProto "go.mau.fi/whatsmeow/binary/proto"
)

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

func TestImageSizeIsSet(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 100, 300))); err != nil {
		t.Fatal(err)
	}
	img := &waProto.ImageMessage{}
	setImageSize(img, buf.Bytes())
	if img.GetWidth() != 100 || img.GetHeight() != 300 {
		t.Fatalf("got %dx%d, want 100x300", img.GetWidth(), img.GetHeight())
	}
	bad := &waProto.ImageMessage{}
	setImageSize(bad, []byte("not an image"))
	if bad.Width != nil {
		t.Fatal("size set for data that isn't an image")
	}
}
