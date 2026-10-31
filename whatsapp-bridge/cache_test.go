package main
import (
 "crypto/sha256"
 "os"
 "path/filepath"
 "testing"
)
func TestMediaCacheRejectsWrongAttachment(t *testing.T) {
 path := filepath.Join(t.TempDir(), "shared.jpg")
 if err := os.WriteFile(path, []byte("first"), 0600); err != nil { t.Fatal(err) }
 expected := sha256.Sum256([]byte("other"))
 if mediaFileMatches(path, expected[:], 5) { t.Fatal("accepted different attachment with same name and length") }
 correct := sha256.Sum256([]byte("first"))
 if !mediaFileMatches(path, correct[:], 5) { t.Fatal("rejected matching cache") }
 if mediaFileMatches(path, correct[:], 6) { t.Fatal("accepted truncated cache") }
 if mediaFileMatches(path, nil, 5) { t.Fatal("accepted unverifiable cache") }
}
