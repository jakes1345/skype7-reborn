package chat

import "testing"

// µ-law is lossy but the round-trip error for linear samples should be bounded
// and monotonic in sign. This test catches encoder bugs like sign inversion or
// segment arithmetic errors.
func TestULawRoundTrip(t *testing.T) {
	cases := []int16{
		0, 1, -1, 100, -100, 1000, -1000, 8000, -8000,
		16000, -16000, 32000, -32000, 32767, -32768,
	}
	for _, sample := range cases {
		enc := linearToULaw(sample)
		dec := ulawToLinear(enc)

		// Sign must be preserved for anything outside the quantization dead zone.
		if sample > 256 && dec <= 0 {
			t.Errorf("sign lost: %d -> 0x%02x -> %d", sample, enc, dec)
		}
		if sample < -256 && dec >= 0 {
			t.Errorf("sign lost: %d -> 0x%02x -> %d", sample, enc, dec)
		}

		// Magnitude should be within G.711 quantization error (~12%).
		abs := int32(sample)
		if abs < 0 {
			abs = -abs
		}
		absDec := int32(dec)
		if absDec < 0 {
			absDec = -absDec
		}
		tolerance := abs/8 + 256
		if absDec > abs+tolerance || absDec < abs-tolerance {
			t.Errorf("round-trip drift: %d -> 0x%02x -> %d (tol=%d)", sample, enc, dec, tolerance)
		}
	}
}

func TestPCMUFrameSize(t *testing.T) {
	pcm := make([]int16, audioFrameSize)
	for i := range pcm {
		pcm[i] = int16(i * 100)
	}
	enc := encodePCMUFrame(pcm)
	if len(enc) != audioFrameSize {
		t.Fatalf("encoded size %d, want %d", len(enc), audioFrameSize)
	}
	dec := decodePCMUFrame(enc)
	if len(dec) != audioFrameSize {
		t.Fatalf("decoded size %d, want %d", len(dec), audioFrameSize)
	}
}
