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

// Process decodes an image, downscales the longest edge to maxEdge, and
// returns the re-encoded full JPEG (q82) and a 320px-wide thumbnail (q75).
func Process(r io.Reader, maxEdge int) (full []byte, thumb []byte, err error) {
	img, _, err := image.Decode(r)
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
