package scan

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/disintegration/imaging"
)

// HashImage computes the SHA-256 hex of the raw base64 image bytes.
func HashImage(b64OrDataURL string) string {
	b := dataURLToRawBase64(b64OrDataURL)
	sum := sha256.Sum256([]byte(b))
	return hex.EncodeToString(sum[:])
}

// AverageHash computes an 8x8 average hash (aHash) of the image.
func AverageHash(b64OrDataURL string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(dataURLToRawBase64(b64OrDataURL))
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	img, _, err := image.Decode(strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}
	gray := imaging.Grayscale(imaging.Resize(img, 8, 8, imaging.Lanczos))

	var sum uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			_, _, b, _ := gray.At(x, y).RGBA()
			sum += uint64(b >> 8)
		}
	}
	mean := sum / 64

	var bits uint64
	i := 0
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			_, _, b, _ := gray.At(x, y).RGBA()
			if uint64(b>>8) > mean {
				bits |= 1 << (63 - i)
			}
			i++
		}
	}
	return fmt.Sprintf("%016x", bits), nil
}

// HammingDistance returns the Hamming distance between two 16-char hex hashes.
func HammingDistance(a, b string) int {
	if len(a) != len(b) || len(a) != 16 {
		return math.MaxInt32
	}
	dist := 0
	for i := 0; i < 16; i++ {
		dist += nibbleDistance(a[i], b[i])
	}
	return dist
}

func nibbleDistance(a, b byte) int {
	x := hexValue(a) ^ hexValue(b)
	count := 0
	for x > 0 {
		count += int(x & 1)
		x >>= 1
	}
	return count
}

func hexValue(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func dataURLToRawBase64(b64OrDataURL string) string {
	if strings.HasPrefix(b64OrDataURL, "data:") {
		parts := strings.SplitN(b64OrDataURL, ",", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	return b64OrDataURL
}
