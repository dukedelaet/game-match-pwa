package media

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"io"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
	_ "image/png"
)

// ErrBadImage means the bytes were not a decodable jpeg/png/webp.
var ErrBadImage = errors.New("could not read image")

// maxPixels bounds the decoded bitmap. The upload byte cap is the wrong
// control: a solid-colour PNG of 12000x12000 compresses to well under a
// megabyte but allocates ~614 MB as RGBA once decoded, and the dimension check
// below only runs after that allocation. A couple of concurrent uploads from
// throwaway signups is enough to OOM the process.
const maxPixels = 40_000_000 // ~6400x6250

// Process decodes an image, downscales the longest edge to maxEdge, and
// returns the re-encoded full JPEG (q82) and a 320px-wide thumbnail (q75).
func Process(r io.Reader, maxEdge int) (full []byte, thumb []byte, err error) {
	// Read the header first and reject oversized images before decoding. The
	// reader must be re-readable for the real decode, so buffer what was read.
	header, rest, err := readHeader(r)
	if err != nil {
		return nil, nil, ErrBadImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(header))
	if err != nil {
		return nil, nil, ErrBadImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, nil, ErrBadImage
	}

	img, _, err := image.Decode(io.MultiReader(bytes.NewReader(header), rest))
	if err != nil {
		return nil, nil, ErrBadImage
	}
	src := img
	if b := img.Bounds(); b.Dx() > maxEdge || b.Dy() > maxEdge {
		src = imaging.Fit(img, maxEdge, maxEdge, imaging.Lanczos)
	}

	var fullBuf bytes.Buffer
	if err := jpeg.Encode(&fullBuf, src, &jpeg.Options{Quality: 82}); err != nil {
		return nil, nil, err
	}

	var thumbSrc image.Image = imaging.Resize(src, 320, 0, imaging.Lanczos)
	if thumbSrc.Bounds().Dy() < 1 {
		thumbSrc = src
	}
	var thumbBuf bytes.Buffer
	if err := jpeg.Encode(&thumbBuf, thumbSrc, &jpeg.Options{Quality: 75}); err != nil {
		return nil, nil, err
	}
	return fullBuf.Bytes(), thumbBuf.Bytes(), nil
}

// headerReadSize is enough for the image headers decode needs: PNG and JPEG
// put their dimensions in the first few hundred bytes, and WebP in the RIFF
// container near the front.
const headerReadSize = 512

// readHeader peeks the first bytes of r and returns them alongside the rest,
// so the caller can inspect the header and then decode the whole stream without
// re-reading it.
func readHeader(r io.Reader) (header []byte, rest io.Reader, err error) {
	buf := make([]byte, headerReadSize)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, nil, err
	}
	header = buf[:n]
	// The prefix has already been consumed from r, so rest is just what follows
	// it. The caller replays the prefix with a MultiReader of its own.
	return header, r, nil
}
