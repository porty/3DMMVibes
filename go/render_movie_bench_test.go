package mm

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"testing"
)

const hospitalSHA256 = "3396bcb84574e7eae13746a59c8ad1ee8a368ac95668c4708bdf60133a90e99b"

func BenchmarkRenderMovieRGB24(b *testing.B) {
	const path = "testdata/HOSPITAL.3MM"

	data, err := os.ReadFile(path)
	if err != nil {
		b.Skipf("skipping: cannot read %s: %v", path, err)
	}

	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	if sum != hospitalSHA256 {
		b.Fatalf("unexpected SHA256 for %s:\n  got  %s\n  want %s", path, sum, hospitalSHA256)
	}

	r := bytes.NewReader(data)
	cf, err := ParseChunkyFile(r)
	if err != nil {
		b.Fatalf("ParseChunkyFile: %v", err)
	}

	b.ResetTimer()
	for b.Loop() {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			b.Fatalf("seek: %v", err)
		}
		if err := RenderMovieRGB24(io.Discard, -1, "", cf, r); err != nil {
			b.Fatalf("RenderMovieRGB24: %v", err)
		}
	}
}
