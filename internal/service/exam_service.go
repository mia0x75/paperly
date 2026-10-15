package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paperly/internal/domain"
	"paperly/internal/storage"
)

// ExamService handles exam upload, query, update, and soft-delete.
type ExamService struct {
	repo     domain.ExamRepository
	pageRepo domain.PageRepository
	dataDir  string
	// txm wraps the exam-row + page-row writes of Upload in one transaction.
	// Wired separately so callers that never upload keep their constructor
	// signature (same pattern as KidService's constructor-injected triple).
	txm domain.TxManager
	// image supplies the WebP compression tier used during upload (adjustable on the settings page).
	image *ImageSettings
}

// NewExamService constructs the exam service. dataDir is the runtime data root; image
// supplies the compression tier. A nil image falls back to the balanced tier so the
// service never becomes unusable for lack of config.
func NewExamService(repo domain.ExamRepository, pageRepo domain.PageRepository, dataDir string, image *ImageSettings) *ExamService {
	if image == nil {
		image = NewImageSettings(0, "")
	}
	return &ExamService{repo: repo, pageRepo: pageRepo, dataDir: dataDir, image: image}
}

// WithTxManager wires the transaction manager needed by the atomic upload path
// (exam row + page rows in one transaction). Kept separate from NewExamService
// so existing callers (and tests) that never upload keep their constructor signature.
func (s *ExamService) WithTxManager(txm domain.TxManager) *ExamService {
	s.txm = txm
	return s
}

// MaxUploadPages caps the page images of one upload (also the post-ops total,
// see maxPagesAfterOps).
const MaxUploadPages = 50

type PageFile struct {
	Reader   io.Reader
	FileName string
	FileSize int64
}

type UploadExamReq struct {
	Pages         []PageFile // at least 1 page, at most MaxUploadPages
	Title         *string    // exam title; the frontend pre-fills via genTitle, editable or empty
	SubjectID     *int64
	Grade         *int    // 1~12
	ExamType      *int    // enum, 0 or a valid value
	ExamDate      *string // "YYYY-MM-DD"
	KidID         *int64  // attribution; NULL = unattributed
	UploadedBy    int     // 1=parent 2=kid (decided by the handler from identity)
	Summary       *string
	SummarySource *int
}

// UpdateExamReq is the input for updating exam metadata (whole-record PUT: nil clears the field).
type UpdateExamReq struct {
	Title         *string
	SubjectID     *int64
	Grade         *int
	ExamType      *int
	ExamDate      *string
	KidID         *int64
	Summary       *string
	SummarySource *int
}

// Upload uploads an exam: validate → read all pages and hash → INSERT exams → write
// files → INSERT pages. The exam row and the page rows share one transaction: any
// failure rolls the rows back and removes written files, leaving neither orphan page
// rows nor a file-less husk exam.
func (s *ExamService) Upload(ctx context.Context, req UploadExamReq) (*domain.Exam, int64, error) {
	// 1. Validate
	if len(req.Pages) == 0 {
		return nil, 0, fmt.Errorf("%w: at least 1 page image required", domain.ErrInvalidInput)
	}
	if len(req.Pages) > MaxUploadPages {
		return nil, 0, fmt.Errorf("%w: no more than %d pages per upload", domain.ErrInvalidInput, MaxUploadPages)
	}
	if req.Grade != nil && (*req.Grade < domain.GradeMin || *req.Grade > domain.GradeMax) {
		return nil, 0, fmt.Errorf("%w: grade must be %d-%d", domain.ErrInvalidInput, domain.GradeMin, domain.GradeMax)
	}
	if err := validateAttribution(req.KidID); err != nil {
		return nil, 0, err
	}
	if err := validateAttribution(req.SubjectID); err != nil {
		return nil, 0, err
	}
	if err := validateSummarySource(req.SummarySource); err != nil {
		return nil, 0, err
	}

	examDate, err := parseDate(req.ExamDate)
	if err != nil {
		return nil, 0, err
	}

	// 2. Read all images: sniff type → decode (EXIF orientation) → cap long edge at
	//    5120 → convert to WebP. The stored sha256 hashes the post-transcode WebP bytes:
	//    those are what live on disk, so hashing the original image would break
	//    integrity verification.
	type stagedPage struct {
		blob   []byte
		ext    string
		sha256 string
	}
	staged := make([]stagedPage, 0, len(req.Pages))
	for i, pg := range req.Pages {
		ext, sniffed, err := storage.SniffExtension(pg.Reader)
		if err != nil {
			return nil, 0, fmt.Errorf("page %d: %w", i+1, err)
		}
		if ext == "" {
			return nil, 0, fmt.Errorf("page %d: %w", i+1, domain.ErrUnsupportedFileType)
		}
		if ext == "pdf" {
			return nil, 0, domain.ErrPDFNotSupported
		}
		blob, err := io.ReadAll(io.LimitReader(sniffed, storage.MaxUploadBytes+1))
		if err != nil {
			return nil, 0, fmt.Errorf("read page %d: %w", i+1, err)
		}
		if len(blob) > storage.MaxUploadBytes {
			return nil, 0, fmt.Errorf("%w: page exceeds size limit", domain.ErrFileTooLarge)
		}
		// WebP conversion: EXIF orientation → scale long edge to 5120 → encode at the
		// configured tier.
		webpBytes, err := storage.ImageToWebP(bytes.NewReader(blob), storage.WebPMaxEdge, s.image.Preset())
		if err != nil {
			return nil, 0, fmt.Errorf("page %d: %w", i+1, domain.ErrInvalidInput)
		}
		staged = append(staged, stagedPage{blob: webpBytes, ext: "webp", sha256: sha256Hex(webpBytes)})
	}

	// 3. Persist in one transaction: INSERT exams → write
	//    assets/exams/{id}/page_NNN.ext → INSERT pages. File writes sit inside the
	//    transaction window on purpose: on any failure the rows roll back and the
	//    directory is removed, so no partial state survives.
	if s.txm == nil {
		return nil, 0, fmt.Errorf("upload exam: write path not configured")
	}
	p := &domain.Exam{
		Title:         req.Title,
		SubjectID:     req.SubjectID,
		Grade:         req.Grade,
		ExamType:      req.ExamType,
		ExamDate:      examDate,
		KidID:         req.KidID,
		UploadedBy:    req.UploadedBy,
		Summary:       req.Summary,
		SummarySource: req.SummarySource,
		Status:        domain.ExamStatusActive,
	}
	var id int64
	var dir string // created inside the tx; swept on any failure (see below)
	err = s.txm.WithinTx(ctx, func(tx domain.Tx) error {
		examRepo := s.repo.WithTx(tx)
		pageRepo := s.pageRepo.WithTx(tx)
		newID, err := examRepo.Insert(ctx, p)
		if err != nil {
			return fmt.Errorf("insert exam: %w", err)
		}
		id = newID
		dir = storage.PageDir(s.dataDir, id)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("mkdir exam dir: %w", err)
		}
		for i, st := range staged {
			pagePath := storage.PagePath(s.dataDir, id, i+1, st.ext)
			if err := os.WriteFile(pagePath, st.blob, 0o640); err != nil {
				_ = os.RemoveAll(dir)
				return fmt.Errorf("write page %d: %w", i+1, err)
			}
		}

		for i, st := range staged {
			page := &domain.Page{
				ExamID:    id,
				PageIndex: i + 1,
				FilePath:  storage.PagePath(s.dataDir, id, i+1, st.ext),
				SHA256:    st.sha256,
			}
			if _, err := pageRepo.Insert(ctx, page); err != nil {
				_ = os.RemoveAll(dir)
				return fmt.Errorf("insert page %d: %w", i+1, err)
			}
		}
		return nil
	})
	if err != nil {
		// fn failures already removed the dir; a commit failure (rows rolled back but the
		// files written) happens outside fn, so sweep it here — no partial state survives.
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		return nil, 0, err
	}

	// 4. Reload
	created, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, id, fmt.Errorf("reload created exam: %w", err)
	}
	return created, id, nil
}

// List cursor-queries exams, returning this page plus the next cursor.
func (s *ExamService) List(ctx context.Context, f domain.ListExamFilter) ([]*domain.Exam, int64, error) {
	return s.repo.List(ctx, f)
}

// Get returns the exam by id (soft-deleted ones are also reachable; List filters visibility).
func (s *ExamService) Get(ctx context.Context, id int64) (*domain.Exam, error) {
	p, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *ExamService) GetDetail(ctx context.Context, id int64) (*domain.Exam, []*domain.Page, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	pages, err := s.pageRepo.ListByExam(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("list exam pages: %w", err)
	}
	return p, pages, nil
}

// PageFileResult is the page-image read result: bytes + inferred Content-Type + owning exam.
// ExamID lets the caller verify ownership (kid audience must check the exam's kid_id; see handler).
type PageFileResult struct {
	Data        []byte
	ContentType string
	ExamID      int64
}

// GetPageFile reads a page's raw image bytes for preview/download.
//
// Security: file_path comes from the DB and should in theory be inside dataDir; a
// containment check is still done to prevent reading files outside dataDir if the DB
// is polluted or a migration writes a bad path (directory traversal).
func (s *ExamService) GetPageFile(ctx context.Context, pageID int64) (*PageFileResult, error) {
	page, err := s.pageRepo.GetByID(ctx, pageID)
	if err != nil {
		return nil, err
	}

	absData, err := filepath.Abs(s.dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	absFile, err := filepath.Abs(page.FilePath)
	if err != nil {
		return nil, fmt.Errorf("resolve page path: %w", err)
	}
	if !strings.HasPrefix(absFile, absData+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: page path outside data dir", domain.ErrInvalidInput)
	}

	data, err := os.ReadFile(absFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("read page file: %w", err)
	}

	ct := "application/octet-stream"
	if ext := strings.ToLower(filepath.Ext(absFile)); ext != "" {
		if m, ok := contentTypeByExt[ext]; ok {
			ct = m
		}
	}
	return &PageFileResult{Data: data, ContentType: ct, ExamID: page.ExamID}, nil
}

// contentTypeByExt covers only the image types this project allows (storage whitelist).
var contentTypeByExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// Update updates exam metadata (whole-record PUT: nil clears the field).
func (s *ExamService) Update(ctx context.Context, id int64, req UpdateExamReq) (*domain.Exam, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if req.Grade != nil && (*req.Grade < domain.GradeMin || *req.Grade > domain.GradeMax) {
		return nil, fmt.Errorf("%w: grade must be %d-%d", domain.ErrInvalidInput, domain.GradeMin, domain.GradeMax)
	}
	if err := validateAttribution(req.KidID); err != nil {
		return nil, err
	}
	if err := validateAttribution(req.SubjectID); err != nil {
		return nil, err
	}
	if err := validateSummarySource(req.SummarySource); err != nil {
		return nil, err
	}

	examDate, err := parseDate(req.ExamDate)
	if err != nil {
		return nil, err
	}

	existing.Title = req.Title
	existing.SubjectID = req.SubjectID
	existing.Grade = req.Grade
	existing.ExamType = req.ExamType
	existing.ExamDate = examDate
	existing.KidID = req.KidID
	existing.Summary = req.Summary
	existing.SummarySource = req.SummarySource

	if err := s.repo.Update(ctx, existing); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("update exam: %w", err)
	}

	return s.Get(ctx, id)
}

// Delete soft-deletes the exam (disk files are kept; hard deletion belongs to a
// cascade flow implemented in a later phase).
func (s *ExamService) Delete(ctx context.Context, id int64) error {
	err := s.repo.SoftDelete(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("delete exam: %w", err)
	}
	return nil
}

// Restore brings a soft-deleted exam back to active.
//
// Only status=deleted exams can be restored; calling it on an active exam →
// ErrInvalidState, so restoring an undeleted exam is not silently treated as success.
// Attribution is unrestricted: restore and delete are a symmetric pair — whoever can
// delete can restore (kid-side attribution checks live in the handler, same basis as
// Delete).
func (s *ExamService) Restore(ctx context.Context, id int64) (*domain.Exam, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status != domain.ExamStatusDeleted {
		return nil, fmt.Errorf("%w: exam is not deleted, nothing to restore", domain.ErrInvalidState)
	}
	if err := s.repo.Restore(ctx, id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("restore exam: %w", err)
	}
	return s.Get(ctx, id)
}

func sha256Hex(blob []byte) string {
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])
}

// parseDate parses "YYYY-MM-DD" (exam_date input); nil is returned for a nil pointer
// or an empty string.
func parseDate(s *string) (*time.Time, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(*s))
	if err != nil {
		return nil, fmt.Errorf("%w: exam date must be YYYY-MM-DD", domain.ErrInvalidInput)
	}
	return &t, nil
}

// validateAttribution validates attribution foreign keys (kid_id / subject_id): nil =
// unset, allowed; a non-nil value must be a positive integer, since 0/negative would
// trigger a foreign-key error or make the exam silently vanish from student lists.
func validateAttribution(id *int64) error {
	if id != nil && *id <= 0 {
		return fmt.Errorf("%w: owner id must be a positive integer", domain.ErrInvalidInput)
	}
	return nil
}

// validateSummarySource rejects out-of-range summary_source values so a tampered
// request cannot persist an unknown provenance (nil = unset, allowed).
func validateSummarySource(v *int) error {
	if v != nil && !domain.SummarySource(*v).Valid() {
		return fmt.Errorf("%w: invalid summary source", domain.ErrInvalidInput)
	}
	return nil
}
