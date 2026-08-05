// Package image provides image processing utilities: resizing, thumbnails,
// cropping, and compression for screenshots, avatars, and attachments.
package image

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Format represents supported image formats.
type Format string

const (
	FormatPNG  Format = "png"
	FormatJPEG Format = "jpeg"
)

// ResizeOptions controls image resizing behavior.
type ResizeOptions struct {
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Quality int    `json:"quality"` // JPEG quality 1-100, default 85
	Fit     string `json:"fit"`     // "cover", "contain", "fill" (default "contain")
	Format  Format `json:"format"`  // "png", "jpeg"
}

// Info holds basic image metadata.
type Info struct {
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Format    Format `json:"format"`
	SizeBytes int64  `json:"size_bytes"`
}

// Decode reads and decodes an image from a reader.
func Decode(r io.Reader) (image.Image, Format, error) {
	img, fmtStr, err := image.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("decode: %w", err)
	}
	var f Format
	switch strings.ToLower(fmtStr) {
	case "png":
		f = FormatPNG
	case "jpeg", "jpg":
		f = FormatJPEG
	default:
		f = FormatPNG
	}
	return img, f, nil
}

// DecodeFile reads and decodes an image from a file path.
func DecodeFile(path string) (image.Image, Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	return Decode(f)
}

// GetInfo returns image metadata for a file.
func GetInfo(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()

	cfg, fmtStr, err := image.DecodeConfig(f)
	if err != nil {
		return Info{}, fmt.Errorf("decode config: %w", err)
	}

	fi, _ := f.Stat()
	var frmt Format
	switch strings.ToLower(fmtStr) {
	case "png":
		frmt = FormatPNG
	case "jpeg", "jpg":
		frmt = FormatJPEG
	}

	return Info{
		Width:     cfg.Width,
		Height:    cfg.Height,
		Format:    frmt,
		SizeBytes: fi.Size(),
	}, nil
}

// Resize resizes an image to the given dimensions using bilinear interpolation.
func Resize(src image.Image, opts ResizeOptions) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	if opts.Width == 0 && opts.Height == 0 {
		return src
	}

	dstW, dstH := computeDimensions(srcW, srcH, opts)
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	// Bilinear interpolation scaling
	for dy := 0; dy < dstH; dy++ {
		for dx := 0; dx < dstW; dx++ {
			// Map destination pixel to source coordinates
			sx := float64(dx) * float64(srcW) / float64(dstW)
			sy := float64(dy) * float64(srcH) / float64(dstH)

			x0 := int(sx)
			y0 := int(sy)
			x1 := minInt(x0+1, srcW-1)
			y1 := minInt(y0+1, srcH-1)

			fx := sx - float64(x0)
			fy := sy - float64(y0)

			// Sample four corners
			c00 := src.At(x0+srcBounds.Min.X, y0+srcBounds.Min.Y)
			c10 := src.At(x1+srcBounds.Min.X, y0+srcBounds.Min.Y)
			c01 := src.At(x0+srcBounds.Min.X, y1+srcBounds.Min.Y)
			c11 := src.At(x1+srcBounds.Min.X, y1+srcBounds.Min.Y)

			// Bilinear blend
			r00, g00, b00, a00 := c00.RGBA()
			r10, g10, b10, a10 := c10.RGBA()
			r01, g01, b01, a01 := c01.RGBA()
			r11, g11, b11, a11 := c11.RGBA()

			r := bilinearBlend(r00, r10, r01, r11, fx, fy)
			g := bilinearBlend(g00, g10, g01, g11, fx, fy)
			b := bilinearBlend(b00, b10, b01, b11, fx, fy)
			a := bilinearBlend(a00, a10, a01, a11, fx, fy)

			dst.SetRGBA(dx, dy, color.RGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: uint8(a >> 8),
			})
		}
	}
	return dst
}

func bilinearBlend(c00, c10, c01, c11 uint32, fx, fy float64) uint32 {
	top := float64(c00)*(1-fx) + float64(c10)*fx
	bottom := float64(c01)*(1-fx) + float64(c11)*fx
	return uint32(top*(1-fy) + bottom*fy)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ResizeFile resizes an image file and writes the result to outPath.
func ResizeFile(inPath, outPath string, opts ResizeOptions) error {
	src, _, err := DecodeFile(inPath)
	if err != nil {
		return err
	}

	if opts.Width <= 0 {
		opts.Width = src.Bounds().Dx()
	}
	if opts.Height <= 0 {
		opts.Height = src.Bounds().Dy()
	}
	if opts.Quality <= 0 {
		opts.Quality = 85
	}
	if opts.Format == "" {
		opts.Format = FormatJPEG
	}
	if opts.Fit == "" {
		opts.Fit = "contain"
	}

	dst := Resize(src, opts)
	return saveImage(outPath, dst, opts)
}

// Thumbnail creates a thumbnail (max 256x256) from an image file.
func Thumbnail(inPath, outPath string) error {
	return ResizeFile(inPath, outPath, ResizeOptions{
		Width:   256,
		Height:  256,
		Fit:     "contain",
		Format:  FormatJPEG,
		Quality: 80,
	})
}

// Crop crops an image to the specified rectangle.
func Crop(src image.Image, x, y, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	cropRect := image.Rect(x, y, x+width, y+height)
	draw.Draw(dst, dst.Bounds(), src, cropRect.Min, draw.Over)
	return dst
}

// Compress reduces file size while maintaining reasonable quality.
func Compress(inPath, outPath string, quality int) error {
	src, _, err := DecodeFile(inPath)
	if err != nil {
		return err
	}
	opts := ResizeOptions{
		Width:   src.Bounds().Dx(),
		Height:  src.Bounds().Dy(),
		Quality: quality,
		Format:  FormatJPEG,
	}
	dst := Resize(src, opts)
	return saveImage(outPath, dst, opts)
}

func computeDimensions(srcW, srcH int, opts ResizeOptions) (int, int) {
	targetW := opts.Width
	targetH := opts.Height
	if targetW <= 0 {
		targetW = int(float64(targetH) * float64(srcW) / float64(srcH))
	}
	if targetH <= 0 {
		targetH = int(float64(targetW) * float64(srcH) / float64(srcW))
	}

	switch opts.Fit {
	case "cover":
		scale := max(float64(targetW)/float64(srcW), float64(targetH)/float64(srcH))
		return int(float64(srcW) * scale), int(float64(srcH) * scale)
	case "fill":
		return targetW, targetH
	default: // "contain"
		scale := min(float64(targetW)/float64(srcW), float64(targetH)/float64(srcH))
		return int(float64(srcW) * scale), int(float64(srcH) * scale)
	}
}

func saveImage(path string, img image.Image, opts ResizeOptions) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer f.Close()

	switch opts.Format {
	case FormatPNG:
		return png.Encode(f, img)
	default:
		return jpeg.Encode(f, img, &jpeg.Options{Quality: opts.Quality})
	}
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
