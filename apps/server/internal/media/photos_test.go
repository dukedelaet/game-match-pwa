package media

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func imageBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 64, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestProcessDownscalesLongestEdge(t *testing.T) {
	full, thumb, err := Process(bytes.NewReader(imageBytes(t, 2000, 1200)), 1080)
	require.NoError(t, err)

	fullCfg, _, err := image.DecodeConfig(bytes.NewReader(full))
	require.NoError(t, err)
	require.Equal(t, 1080, fullCfg.Width)
	require.Equal(t, 648, fullCfg.Height)

	thumbCfg, _, err := image.DecodeConfig(bytes.NewReader(thumb))
	require.NoError(t, err)
	require.Equal(t, 320, thumbCfg.Width)
}

func TestProcessLeavesSmallImagesAlone(t *testing.T) {
	full, _, err := Process(bytes.NewReader(imageBytes(t, 200, 100)), 1080)
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(full))
	require.NoError(t, err)
	require.Equal(t, 200, cfg.Width)
	require.Equal(t, 100, cfg.Height)
}

func TestProcessRejectsNonImage(t *testing.T) {
	_, _, err := Process(strings.NewReader("definitely not an image"), 1080)
	require.ErrorIs(t, err, ErrBadImage)
}

func TestProcessRejectsGIF(t *testing.T) {
	// The decoder registry has no GIF support here, so a GIF header is rejected.
	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00")
	_, _, err := Process(bytes.NewReader(gif), 1080)
	require.ErrorIs(t, err, ErrBadImage)
}

// solidPNG encodes a single-colour image. Such an image compresses to almost
// nothing while describing a bitmap large enough to exhaust memory, which is
// the whole point of the attack: a few hundred KB on the wire, gigabytes once
// decoded.
func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x40
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestProcessRejectsDecompressionBomb(t *testing.T) {
	payload := solidPNG(t, 12000, 12000)
	require.Less(t, len(payload), 2<<20, "the payload must be small on the wire")

	track := &peakReader{r: bytes.NewReader(payload)}

	_, _, err := Process(track, 1080)
	require.ErrorIs(t, err, ErrBadImage)
	require.Less(t, track.peak, uint64(64<<20),
		"a bomb must be rejected from its header, not after allocating the bitmap")
}

// A large but legitimate photo must still be downscaled, not rejected: the
// ceiling guards memory, it does not cap upload resolution below maxEdge.
func TestProcessAcceptsLargeButReasonableImage(t *testing.T) {
	full, _, err := Process(bytes.NewReader(solidPNG(t, 4000, 3000)), 1080)
	require.NoError(t, err)

	cfg, _, err := image.DecodeConfig(bytes.NewReader(full))
	require.NoError(t, err)
	require.Equal(t, 1080, cfg.Width)
}

// peakReader records how far into the payload the decoder actually read. A
// header-only rejection reads almost none of it.
type peakReader struct {
	r    *bytes.Reader
	peak uint64
}

func (p *peakReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if uint64(n) > p.peak {
		p.peak = uint64(n)
	}
	return n, err
}
