package image

import (
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func createTestImage(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "oh-img-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}

	path := filepath.Join(dir, "test.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return path, func() { os.RemoveAll(dir) }
}

func TestGetInfo(t *testing.T) {
	path, cleanup := createTestImage(t)
	defer cleanup()

	info, err := GetInfo(path)
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if info.Width != 400 || info.Height != 300 {
		t.Fatalf("expected 400x300, got %dx%d", info.Width, info.Height)
	}
	if info.Format != FormatJPEG {
		t.Fatalf("expected jpeg, got %s", info.Format)
	}
}

func TestDecodeFile(t *testing.T) {
	path, cleanup := createTestImage(t)
	defer cleanup()

	img, f, err := DecodeFile(path)
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if f != FormatJPEG {
		t.Fatalf("expected jpeg, got %s", f)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 300 {
		t.Fatalf("expected 400x300, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestResize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))

	resized := Resize(img, ResizeOptions{Width: 200, Height: 150, Fit: "contain"})
	bounds := resized.Bounds()

	if bounds.Dx() != 200 || bounds.Dy() != 150 {
		t.Fatalf("expected 200x150, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestResizeContain(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 400))

	// W=400,H=400 with contain should give 400x200 (maintaining 2:1 ratio)
	resized := Resize(img, ResizeOptions{Width: 400, Height: 400, Fit: "contain"})
	bounds := resized.Bounds()

	if bounds.Dx() != 400 || bounds.Dy() != 200 {
		t.Fatalf("expected 400x200, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestCrop(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	cropped := Crop(img, 10, 10, 80, 80)

	bounds := cropped.Bounds()
	if bounds.Dx() != 80 || bounds.Dy() != 80 {
		t.Fatalf("expected 80x80, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestThumbnail(t *testing.T) {
	path, cleanup := createTestImage(t)
	defer cleanup()

	thumbPath := filepath.Join(filepath.Dir(path), "thumb.jpg")
	if err := Thumbnail(path, thumbPath); err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}

	info, err := GetInfo(thumbPath)
	if err != nil {
		t.Fatalf("GetInfo thumb: %v", err)
	}
	if info.Width > 256 || info.Height > 256 {
		t.Fatalf("thumbnail too large: %dx%d", info.Width, info.Height)
	}
}

func TestComputeDimensions(t *testing.T) {
	w, h := computeDimensions(800, 600, ResizeOptions{Width: 200, Height: 200, Fit: "contain"})
	// 800/600 = 4/3. Container should be 200x150
	if w != 200 || h != 150 {
		t.Fatalf("contain: expected 200x150, got %dx%d", w, h)
	}

	w, h = computeDimensions(800, 600, ResizeOptions{Width: 200, Height: 200, Fit: "cover"})
	// Cover should fill: scale = max(200/800, 200/600) = max(0.25, 0.333) = 0.333
	// 800*0.333=266, 600*0.333=200
	if w != 266 || h != 200 {
		t.Fatalf("cover: expected 266x200, got %dx%d", w, h)
	}
}
