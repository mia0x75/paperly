package service

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"paperly/internal/domain"
)

// Summary size ceilings, counted in bytes of the stored HTML.
//
// The summary is append-only: each appended segment is signed and kept forever, so the
// total only grows. The hard cap keeps a single exam row from bloating without bound
// (the column is TEXT; a runaway summary would slow the list/detail queries and the
// backup).
const (
	// SummaryMaxBytes caps the total stored summary HTML after appending.
	SummaryMaxBytes = 64 * 1024
	// SummarySegmentBytes caps one appended segment's content HTML.
	SummarySegmentBytes = 8 * 1024
)

// summaryTimeLayout is the signed timestamp format ("2026-10-11 09:30").
const summaryTimeLayout = "2006-01-02 15:04"

// SummaryAuthor identifies who is appending/deleting a signed segment.
type SummaryAuthor struct {
	// Type is domain.UploadedByParent / domain.UploadedByKid.
	Type int
	ID   int64
	Name string
}

// summarySegmentTagRe matches a signed segment's opening tag (any <div ... data-sig="...">).
var summarySegmentTagRe = regexp.MustCompile(`<div[^>]*\bdata-sig="[^"]*"[^>]*>`)

// summaryAttrRe extracts key="value" pairs from an opening tag.
var summaryAttrRe = regexp.MustCompile(`([A-Za-z0-9_-]+)="([^"]*)"`)

// summaryTagStripRe strips tags so "empty but wrapped" content (e.g. <p></p>) is
// recognized as empty instead of accepted as a blank segment.
var summaryTagStripRe = regexp.MustCompile(`<[^>]*>`)

// summarySaveMaxRetries bounds the optimistic-concurrency retry loop in
// saveSummary: under contention each retry re-reads the fresh summary, so a
// handful of attempts converges; beyond that the caller is told to retry.
const summarySaveMaxRetries = 3

// AppendSummary appends a signed rich-text segment to the exam summary and persists it.
// It returns the new segment's sig and the full updated summary.
//
// The summary is a plain concatenation of segments: each segment is a
// `<div class="summary-sign" ...>Name · time</div>` marker followed by the segment body.
// There is deliberately no separator; the marker opens every segment, so splitting on
// the marker is unambiguous.
func (s *ExamService) AppendSummary(ctx context.Context, examID int64, author SummaryAuthor, contentHTML string, now time.Time) (string, string, error) {
	if !summaryHasContent(contentHTML) {
		return "", "", fmt.Errorf("%w: summary content cannot be empty", domain.ErrInvalidInput)
	}
	if len(contentHTML) > SummarySegmentBytes {
		return "", "", fmt.Errorf("%w: single summary exceeds %d bytes", domain.ErrInvalidInput, SummarySegmentBytes)
	}

	var sig string
	next, err := s.saveSummary(ctx, examID, func(current string) (string, error) {
		sig = newSummarySig(current)
		cand := current + buildSummarySegment(sig, author, contentHTML, now)
		if len(cand) > SummaryMaxBytes {
			return "", fmt.Errorf("%w: total summary exceeds %d bytes", domain.ErrInvalidInput, SummaryMaxBytes)
		}
		return cand, nil
	})
	if err != nil {
		return "", "", err
	}
	return sig, next, nil
}

// DeleteSummary removes the segment identified by sig and persists the result.
//
// Permission: parents may delete any segment; a kid may only delete one they authored
// (author-type=kid and author-id=their own id). A segment the caller may not delete
// reports ErrNotFound, matching the "not yours = hidden" convention used by exam
// ownership checks, so existence never leaks.
func (s *ExamService) DeleteSummary(ctx context.Context, examID int64, sig string, author SummaryAuthor) (string, error) {
	sig = strings.TrimSpace(sig)
	if sig == "" {
		return "", fmt.Errorf("%w: missing segment identifier", domain.ErrInvalidInput)
	}

	return s.saveSummary(ctx, examID, func(current string) (string, error) {
		span, ok := findSummarySegment(current, sig)
		if !ok {
			return "", domain.ErrNotFound
		}
		if !span.deletableBy(author) {
			return "", domain.ErrNotFound
		}
		return current[:span.start] + current[span.end:], nil
	})
}

// saveSummary persists a recomputed summary via a conditional write (UpdateSummary only
// applies when the stored value still equals what build saw). On a mismatch (concurrent
// append) it re-reads and retries, so two simultaneous appends both land instead of the
// last write silently dropping the first segment. After summarySaveMaxRetries misses it
// reports ErrInvalidState and the caller retries the whole operation.
func (s *ExamService) saveSummary(ctx context.Context, examID int64, build func(current string) (string, error)) (string, error) {
	for i := 0; ; i++ {
		exam, err := s.Get(ctx, examID)
		if err != nil {
			return "", err
		}
		current := ""
		if exam.Summary != nil {
			current = *exam.Summary
		}
		next, err := build(current)
		if err != nil {
			return "", err
		}
		updated, err := s.repo.UpdateSummary(ctx, examID, current, next)
		if err != nil {
			return "", err
		}
		if updated {
			return next, nil
		}
		if i+1 >= summarySaveMaxRetries {
			return "", fmt.Errorf("%w: summary changed concurrently, please retry", domain.ErrInvalidState)
		}
	}
}

// summarySegment is one signed segment's byte span in the summary plus its author metadata.
type summarySegment struct {
	sig        string
	start, end int
	authorType int
	authorID   int64
}

func (seg summarySegment) deletableBy(author SummaryAuthor) bool {
	if author.Type == domain.UploadedByParent {
		return true
	}
	return author.Type == domain.UploadedByKid &&
		seg.authorType == domain.UploadedByKid &&
		seg.authorID == author.ID
}

func findSummarySegment(summary, sig string) (summarySegment, bool) {
	for _, seg := range parseSummarySegments(summary) {
		if seg.sig == sig {
			return seg, true
		}
	}
	return summarySegment{}, false
}

// parseSummarySegments returns every segment with its [start,end) span. A segment
// ends where the next segment's marker begins (or at the end of the summary), so
// the body content that follows a marker belongs to that segment.
func parseSummarySegments(summary string) []summarySegment {
	locs := summarySegmentTagRe.FindAllStringIndex(summary, -1)
	if len(locs) == 0 {
		return nil
	}
	segs := make([]summarySegment, 0, len(locs))
	for i, loc := range locs {
		tag := summary[loc[0]:loc[1]]
		end := len(summary)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		attrs := parseSummaryAttrs(tag)
		segs = append(segs, summarySegment{
			sig:        attrs["data-sig"],
			start:      loc[0],
			end:        end,
			authorType: atoiSafe(attrs["data-author-type"]),
			authorID:   atoi64Safe(attrs["data-author"]),
		})
	}
	return segs
}

func parseSummaryAttrs(tag string) map[string]string {
	out := make(map[string]string)
	for _, m := range summaryAttrRe.FindAllStringSubmatch(tag, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// buildSummarySegment renders the signed marker + body for one appended segment.
// The author name is HTML-escaped: it is user-controlled (parent nickname / kid name)
// and is rendered, so it must never be able to break out of the marker attribute/text.
func buildSummarySegment(sig string, author SummaryAuthor, contentHTML string, now time.Time) string {
	name := html.EscapeString(author.Name)
	marker := fmt.Sprintf(
		`<div class="summary-sign" data-sig="%s" data-author-type="%d" data-author="%d">%s · %s</div>`,
		sig, author.Type, author.ID, name, now.Format(summaryTimeLayout),
	)
	return marker + contentHTML
}

// newSummarySig generates an 8-hex-char segment id (the first 8 hex chars of a
// UUID) and re-rolls on the rare in-exam collision so deletion by sig stays exact.
func newSummarySig(existing string) string {
	for {
		sig := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
		if !strings.Contains(existing, `data-sig="`+sig+`"`) {
			return sig
		}
	}
}

// summaryHasContent reports whether the content has visible text (tags/entities
// and whitespace only = empty).
func summaryHasContent(s string) bool {
	stripped := summaryTagStripRe.ReplaceAllString(s, "")
	text := html.UnescapeString(strings.ReplaceAll(stripped, "\u00a0", ""))
	return strings.TrimSpace(text) != ""
}

// atoiSafe parses a decimal int; invalid/empty returns 0.
func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// atoi64Safe parses a decimal int64; invalid/empty returns 0.
func atoi64Safe(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
