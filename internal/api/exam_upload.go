package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"strconv"

	"github.com/labstack/echo/v5"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime/multipart"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// maxImageLongEdge is the upload-time size rejection threshold: images whose
// long edge exceeds this are rejected with 400 instead of being transcoded
// (a decode + scale on a giant image would exhaust memory on a small host).
// WebP conversion rescales anything up to this edge; only extreme over-sized
// uploads are refused early.
//
// Layer ownership of the three upload size caps: this handler does the cheap
// header-only refusal (DecodeConfig, no full decode); the service layer owns
// the authoritative checks (storage.MaxUploadBytes per-file cap, transcoding
// to storage.WebPMaxEdge). The handler never duplicates the service's limits.
const maxImageLongEdge = 16384

// acquireUploadSlot takes an upload concurrency token, bound to the request
// context: a disconnected client stops waiting instead of occupying the queue.
// A full queue reports 429 (retry later); a cancelled wait reports 500 with an
// explicit message (the response likely never reaches the gone client anyway).
func (h *ExamHandler) acquireUploadSlot(c *echo.Context) error {
	if h.gate == nil {
		return nil
	}
	if err := h.gate.AcquireContext(c.Request().Context()); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fail(c, CodeInternal, "upload cancelled", err.Error())
		}
		return failTooMany(c, "system busy, retry later")
	}
	return nil
}

// Upload handles POST /api/exams/upload (multipart: N file fields + metadata).
func (h *ExamHandler) Upload(c *echo.Context) error {
	if err := h.acquireUploadSlot(c); err != nil {
		return err
	}
	if h.gate != nil {
		defer h.gate.Release()
	}
	form, err := c.MultipartForm()
	if err != nil {
		return fail(c, CodeInvalidBody, "invalid multipart form", err.Error())
	}
	fileHeaders := form.File["file"]
	if len(fileHeaders) == 0 {
		return fail(c, CodeValidation, "missing file field", "")
	}
	pages := make([]service.PageFile, 0, len(fileHeaders))
	for _, fh := range fileHeaders {
		pf, err := readUploadFile(c, fh)
		if err != nil {
			return err
		}
		pages = append(pages, pf)
	}

	req := service.UploadExamReq{
		Pages:      pages,
		Title:      optFormValue(c, "title"),
		Grade:      optIntForm(c, "grade"),
		ExamType:   optIntForm(c, "exam_type"),
		ExamDate:   optFormValue(c, "exam_date"),
		Summary:    optFormValue(c, "summary"),
		UploadedBy: domain.UploadedByParent, // decided server-side (fixed to parent without auth)
	}
	if v := optFormValue(c, "subject_id"); v != nil {
		id, err := strconv.ParseInt(*v, 10, 64)
		if err != nil || id <= 0 {
			return fail(c, CodeValidation, "invalid subject_id", *v)
		}
		req.SubjectID = &id
	}
	aud := httpmw.AudienceFromCtx(c)
	if aud == httpmw.AudienceKid {
		kidID := httpmw.KidIDFromCtx(c)
		if kidID > 0 {
			req.KidID = &kidID
		}
	} else if v := optFormValue(c, "kid_id"); v != nil {
		id, err := strconv.ParseInt(*v, 10, 64)
		if err != nil || id <= 0 {
			return fail(c, CodeValidation, "invalid kid_id", *v)
		}
		req.KidID = &id
	}

	exam, _, err := h.svc.Upload(c.Request().Context(), req)
	if err != nil {
		return mapExamError(c, err)
	}
	return ok(c, dto.NewUploadExamResponse(exam))
}

// readUploadFile opens one multipart file, runs the cheap oversize guard, and
// reads it fully into memory, closing the file immediately: holding up to 50
// open files until the handler returns wastes descriptors, while the staged
// bytes already live in memory downstream anyway.
//
// The failure response is written here; the returned error is only a signal
// (fail writes the response and reports success as nil, so it cannot double
// as the signal).
func readUploadFile(c *echo.Context, fh *multipart.FileHeader) (service.PageFile, error) {
	src, err := fh.Open()
	if err != nil {
		_ = fail(c, CodeInvalidBody, "cannot open uploaded file", err.Error())
		return service.PageFile{}, err
	}
	defer func() { _ = src.Close() }()
	if err := rejectOversizedImage(src); err != nil {
		_ = fail(c, CodeValidation, "image too large", err.Error())
		return service.PageFile{}, err
	}
	blob, err := io.ReadAll(src)
	if err != nil {
		_ = fail(c, CodeInvalidBody, "cannot read uploaded file", err.Error())
		return service.PageFile{}, err
	}
	return service.PageFile{Reader: bytes.NewReader(blob), FileName: fh.Filename, FileSize: fh.Size}, nil
}

// rejectOversizedImage reads the image header via DecodeConfig (cheap, no full
// decode) and rejects images whose long edge exceeds maxImageLongEdge with a
// descriptive error. It rewinds the reader to the start afterwards so the
// caller can still consume the full stream (multipart.File supports Seek).
//
// DecodeConfig ignores EXIF orientation: the raw pixel dimensions decide the
// long edge, and rotation never changes the long edge, so no EXIF lookup is
// needed here. A DecodeConfig failure (corrupt header, oversized fake payload)
// returns nil: the downstream pipeline owns the file-type/size error semantics
// (too-large -> CodeFileTooLarge, non-image -> decode error), so this guard only
// rejects images it can positively identify as over-sized.
func rejectOversizedImage(r io.ReadSeeker) error {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		if _, sErr := r.Seek(0, io.SeekStart); sErr != nil {
			return fmt.Errorf("rewind upload: %w", sErr)
		}
		return nil
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind upload: %w", err)
	}
	longEdge := cfg.Width
	if cfg.Height > longEdge {
		longEdge = cfg.Height
	}
	if longEdge > maxImageLongEdge {
		return fmt.Errorf("image too large (long edge %d > %d px)", longEdge, maxImageLongEdge)
	}
	return nil
}
