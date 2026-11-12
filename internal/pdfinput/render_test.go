package pdfinput

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"
	"time"

	"inbrix/internal/agenttest"
)

func TestPDFRenderPagesAndPixels(t *testing.T) {
	data := agenttest.PDF(2)
	count, err := Default.Inspect(context.Background(), data)
	if err != nil || count != 2 {
		t.Fatalf("inspect: %d %v", count, err)
	}
	var progress []int
	pages, err := Default.Render(context.Background(), data, func(page, total int) error {
		if total != 2 {
			t.Fatal(total)
		}
		progress = append(progress, page)
		return nil
	})
	if err != nil || len(pages) != 2 || len(progress) != 2 {
		t.Fatalf("render: %d %v %v", len(pages), progress, err)
	}
	for index, data := range pages {
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		bounds := img.Bounds()
		if bounds.Dx() > MaxSide || bounds.Dy() > MaxSide || bounds.Dx() < 100 {
			t.Fatal("unbounded or empty render", bounds)
		}
		r, g, b, _ := img.At(bounds.Dx()/4, bounds.Dy()*3/4).RGBA()
		if index == 0 && (r < 50000 || g > 10000 || b > 10000) || index == 1 && (b < 50000 || r > 10000 || g > 10000) {
			t.Fatalf("page %d has incorrect pixels: %d %d %d", index+1, r, g, b)
		}
	}
}

func TestPDFLimitsAndCancellation(t *testing.T) {
	ctx := context.Background()
	for _, data := range [][]byte{nil, []byte("%PDF-1.7\ninvalid\n%%EOF"), agenttest.PDF(MaxPages + 1)} {
		if _, err := Default.Inspect(ctx, data); err == nil {
			t.Fatal("invalid or oversized PDF accepted")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Default.Inspect(canceled, agenttest.PDF(1)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	canceled, cancel = context.WithCancel(ctx)
	pages, err := Default.Render(canceled, agenttest.PDF(2), func(int, int) error { cancel(); return canceled.Err() })
	if !errors.Is(err, context.Canceled) || pages != nil {
		t.Fatal("canceled document published partial pages", err)
	}
	// A canceled worker must not poison later work.
	if count, err := Default.Inspect(ctx, agenttest.PDF(1)); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestPDFCancelInFlight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var timer *time.Timer
	pages, err := Default.Render(ctx, agenttest.PDF(MaxPages), func(page, _ int) error {
		if page == 1 {
			timer = time.AfterFunc(2*time.Millisecond, cancel)
		}
		return nil
	})
	if timer != nil {
		timer.Stop()
	}
	if !errors.Is(err, context.Canceled) || pages != nil {
		t.Fatal("in-flight cancellation failed", len(pages), err)
	}
	if count, err := Default.Inspect(context.Background(), agenttest.PDF(1)); err != nil || count != 1 {
		t.Fatal("renderer did not recover", count, err)
	}
}

func TestPDFScannedImageWithoutTextLayer(t *testing.T) {
	scanImage := image.NewRGBA(image.Rect(0, 0, 300, 400))
	draw.Draw(scanImage, scanImage.Bounds(), image.NewUniform(color.RGBA{G: 255, A: 255}), scanImage.Bounds().Min, draw.Src)
	var scan bytes.Buffer
	if err := jpeg.Encode(&scan, scanImage, nil); err != nil {
		t.Fatal(err)
	}
	pages, err := Default.Render(context.Background(), agenttest.ScannedPDF(scan.Bytes(), 300, 400), nil)
	if err != nil || len(pages) != 1 {
		t.Fatal("scan failed", err)
	}
	rendered, err := jpeg.Decode(bytes.NewReader(pages[0]))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := rendered.At(100, 100).RGBA()
	if g < 50000 || r > 10000 || b > 10000 {
		t.Fatalf("embedded scan lost: %d %d %d", r, g, b)
	}
}
