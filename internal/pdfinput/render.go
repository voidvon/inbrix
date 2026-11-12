// Package pdfinput prepares bounded page images without extracting document text.
package pdfinput

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

const (
	MaxPages         = 20
	MaxSide          = 1800
	MaxDocumentBytes = 10 << 20
	MaxOutputBytes   = 24 << 20
)

type Renderer struct {
	once sync.Once
	pool pdfium.Pool
	err  error
	// One active document bounds WASM heaps and Go image buffers process-wide.
	gate chan struct{}
}

var Default = &Renderer{gate: make(chan struct{}, 1)}

func (r *Renderer) init() {
	r.pool, r.err = webassembly.Init(webassembly.Config{
		MaxTotal: 1,
		FSConfig: wazero.NewFSConfig(), // No host filesystem access, including defaults.
		Stdout:   io.Discard, Stderr: io.Discard,
		RuntimeConfig: wazero.NewRuntimeConfig().
			WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
			WithMemoryLimitPages(4096).WithCloseOnContextDone(true), // 256 MiB per heap.
	})
}

// withDocument discards every worker after use. Cancellation interrupts WASM,
// then joins the native operation before releasing the single-worker reservation.
func (r *Renderer) withDocument(ctx context.Context, data []byte, fn func(context.Context, pdfium.Pdfium, *requests.PageByIndex, int) error) (err error) {
	if len(data) == 0 || len(data) > MaxDocumentBytes {
		return errors.New("PDF 文件大小无效 / Invalid PDF size")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	select {
	case r.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-r.gate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	r.once.Do(r.init)
	if r.err != nil {
		return fmt.Errorf("PDF 渲染引擎不可用 / PDF renderer unavailable: %w", r.err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	instance, err := r.pool.GetInstanceWithContext(ctx)
	if err != nil {
		return fmt.Errorf("PDF 渲染器繁忙 / PDF renderer busy: %w", err)
	}
	// This is the sole owner of instance lifecycle. Kill and Close never overlap.
	finished, watched := make(chan struct{}), make(chan bool, 1)
	go func() {
		select {
		case <-ctx.Done():
			_ = instance.Kill()
			watched <- true
		case <-finished:
			watched <- false
		}
	}()
	defer func() {
		close(finished)
		if killed := <-watched; !killed {
			_ = instance.Close()
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return errors.New("PDF 损坏、已加密或无法读取，请上传未加密的有效 PDF / Cannot open PDF; use an unencrypted valid PDF")
	}
	count, err := instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return errors.New("无法读取 PDF 页数 / Cannot read PDF page count")
	}
	if count.PageCount < 1 || count.PageCount > MaxPages {
		return fmt.Errorf("PDF 共 %d 页；页面图片模式每份最多 %d 页，请拆分文件后上传 / PDF page limit exceeded", count.PageCount, MaxPages)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx, instance, &requests.PageByIndex{Document: doc.Document}, count.PageCount)
}

func (r *Renderer) Inspect(ctx context.Context, data []byte) (pages int, err error) {
	err = r.withDocument(ctx, data, func(_ context.Context, _ pdfium.Pdfium, _ *requests.PageByIndex, count int) error {
		pages = count
		return nil
	})
	return
}

// Render publishes a complete document only; failures never return partial pages.
func (r *Renderer) Render(ctx context.Context, data []byte, progress func(int, int) error) (pages [][]byte, err error) {
	err = r.withDocument(ctx, data, func(ctx context.Context, instance pdfium.Pdfium, page *requests.PageByIndex, count int) error {
		total := 0
		for index := 0; index < count; index++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if progress != nil {
				if err := progress(index+1, count); err != nil {
					return err
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			page.Index = index
			result, err := instance.RenderToFile(&requests.RenderToFile{
				RenderPageInPixels: &requests.RenderPageInPixels{
					Page: requests.Page{ByIndex: page}, Width: MaxSide, Height: MaxSide,
					RenderFlags: enums.FPDF_RENDER_FLAG_ANNOT, RenderForm: true, Document: &page.Document,
				},
				OutputFormat: requests.RenderToFileOutputFormatJPG,
				OutputTarget: requests.RenderToFileOutputTargetBytes, OutputQuality: 90,
			})
			if err != nil || result == nil || result.ImageBytes == nil {
				return fmt.Errorf("PDF 第 %d 页渲染失败 / PDF page %d could not be rendered", index+1, index+1)
			}
			total += len(*result.ImageBytes)
			if total > MaxOutputBytes {
				return errors.New("PDF 页面图片超过大小上限，请拆分文件 / Rendered PDF exceeds size limit; split the file")
			}
			pages = append(pages, *result.ImageBytes)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pages, nil
}
