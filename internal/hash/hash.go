package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// HashFile reads the file at path and returns its SHA-256 hash as a hex-encoded string.
// The file is streamed (io.Copy) so large files are handled without loading into memory.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
