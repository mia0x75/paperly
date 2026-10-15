package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/repository"
)

// summaryNow is a fixed timestamp for deterministic signature assertions.
var summaryNow = time.Date(2026, 10, 11, 9, 30, 0, 0, time.UTC)

func newSummarySvc(t *testing.T) (*ExamService, *mockPaperRepo) {
	t.Helper()
	repo := newMockPaperRepo()
	svc := NewExamService(repo, newMockPageRepo(), t.TempDir(), nil).WithTxManager(fakeTxManager{})
	return svc, repo
}

func seedSummaryExam(t *testing.T, repo *mockPaperRepo, summary *string) int64 {
	t.Helper()
	id, err := repo.Insert(context.Background(), &domain.Exam{Status: domain.ExamStatusActive, Summary: summary})
	require.NoError(t, err)
	return id
}

func TestAppendSummarySuccess(t *testing.T) {
	svc, repo := newSummarySvc(t)
	id := seedSummaryExam(t, repo, nil)

	author := SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"}
	sig, updated, err := svc.AppendSummary(context.Background(), id, author, "<p>这次粗心了</p>", summaryNow)
	require.NoError(t, err)
	require.Len(t, sig, 8)
	require.Contains(t, updated, `data-sig="`+sig+`"`)
	require.Contains(t, updated, `data-author-type="1"`)
	require.Contains(t, updated, `data-author="1"`)
	require.Contains(t, updated, "妈妈 · 2026-10-11 09:30")
	require.Contains(t, updated, "<p>这次粗心了</p>")

	// Persisted to the repository.
	stored, err := repo.GetByID(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored.Summary)
	require.Equal(t, updated, *stored.Summary)
}

// TestAppendSummaryEscapesAuthorName verifies the (user-controlled) author name
// cannot break out of the marker markup.
func TestAppendSummaryEscapesAuthorName(t *testing.T) {
	svc, repo := newSummarySvc(t)
	id := seedSummaryExam(t, repo, nil)

	author := SummaryAuthor{Type: domain.UploadedByKid, ID: 7, Name: `<img src=x onerror=alert(1)>`}
	_, summary, err := svc.AppendSummary(context.Background(), id, author, "<p>hi</p>", summaryNow)
	require.NoError(t, err)
	require.NotContains(t, summary, "<img")
	require.Contains(t, summary, "&lt;img")
}

func TestAppendSummaryRejectsEmptyContent(t *testing.T) {
	svc, repo := newSummarySvc(t)
	id := seedSummaryExam(t, repo, nil)
	author := SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"}

	for _, content := range []string{"", "   ", "<p></p>", "<p>\n</p>", "<p>&nbsp;</p>", "<p>  </p>"} {
		_, _, err := svc.AppendSummary(context.Background(), id, author, content, summaryNow)
		require.ErrorIs(t, err, domain.ErrInvalidInput, "content=%q", content)
	}
}

func TestAppendSummaryRejectsOversizedSegment(t *testing.T) {
	svc, repo := newSummarySvc(t)
	id := seedSummaryExam(t, repo, nil)

	content := "<p>" + strings.Repeat("a", SummarySegmentBytes) + "</p>"
	_, _, err := svc.AppendSummary(context.Background(), id,
		SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"}, content, summaryNow)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestAppendSummaryRejectsTotalOverflow(t *testing.T) {
	svc, repo := newSummarySvc(t)
	existing := strings.Repeat("a", SummaryMaxBytes-4)
	id := seedSummaryExam(t, repo, &existing)

	_, _, err := svc.AppendSummary(context.Background(), id,
		SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"}, "<p>hello</p>", summaryNow)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestAppendSummaryExamNotFound(t *testing.T) {
	svc, _ := newSummarySvc(t)
	_, _, err := svc.AppendSummary(context.Background(), 999,
		SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"}, "<p>x</p>", summaryNow)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAppendSummarySigsAreUnique(t *testing.T) {
	svc, repo := newSummarySvc(t)
	id := seedSummaryExam(t, repo, nil)
	author := SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"}

	sig1, _, err := svc.AppendSummary(context.Background(), id, author, "<p>一</p>", summaryNow)
	require.NoError(t, err)
	sig2, summary, err := svc.AppendSummary(context.Background(), id, author, "<p>二</p>", summaryNow)
	require.NoError(t, err)
	require.NotEqual(t, sig1, sig2)
	require.Len(t, parseSummarySegments(summary), 2)
}

func TestDeleteSummaryParentDeletesAny(t *testing.T) {
	svc, repo := newSummarySvc(t)
	legacy := "<p>旧的纯文本总结</p>"
	id := seedSummaryExam(t, repo, &legacy)

	kid := SummaryAuthor{Type: domain.UploadedByKid, ID: 7, Name: "小明"}
	kidSig, _, err := svc.AppendSummary(context.Background(), id, kid, "<p>孩子写的</p>", summaryNow)
	require.NoError(t, err)
	parent := SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"}
	parentSig, withTwo, err := svc.AppendSummary(context.Background(), id, parent, "<p>家长写的</p>", summaryNow)
	require.NoError(t, err)

	// Parent deletes the kid's segment: the parent's own segment and the legacy prefix remain.
	next, err := svc.DeleteSummary(context.Background(), id, kidSig, parent)
	require.NoError(t, err)
	require.NotContains(t, next, kidSig)
	require.Contains(t, next, parentSig)
	require.True(t, strings.HasPrefix(next, legacy))
	require.NotContains(t, next, "孩子写的")
	require.Contains(t, next, "家长写的")
	require.Less(t, len(next), len(withTwo))
}

func TestDeleteSummaryKidOnlyOwn(t *testing.T) {
	svc, repo := newSummarySvc(t)
	id := seedSummaryExam(t, repo, nil)

	kid := SummaryAuthor{Type: domain.UploadedByKid, ID: 7, Name: "小明"}
	kidSig, _, err := svc.AppendSummary(context.Background(), id, kid, "<p>孩子的</p>", summaryNow)
	require.NoError(t, err)
	parent := SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"}
	parentSig, _, err := svc.AppendSummary(context.Background(), id, parent, "<p>家长的</p>", summaryNow)
	require.NoError(t, err)

	// Kid cannot delete the parent's segment (hidden as not found).
	_, err = svc.DeleteSummary(context.Background(), id, parentSig, kid)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Another kid cannot delete this kid's segment.
	otherKid := SummaryAuthor{Type: domain.UploadedByKid, ID: 8, Name: "小红"}
	_, err = svc.DeleteSummary(context.Background(), id, kidSig, otherKid)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// The author kid can delete their own.
	next, err := svc.DeleteSummary(context.Background(), id, kidSig, kid)
	require.NoError(t, err)
	require.NotContains(t, next, kidSig)
	require.Contains(t, next, parentSig)
}

func TestDeleteSummaryUnknownSig(t *testing.T) {
	svc, repo := newSummarySvc(t)
	id := seedSummaryExam(t, repo, nil)
	_, err := svc.DeleteSummary(context.Background(), id, "deadbeef",
		SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestDeleteSummaryEmptySig(t *testing.T) {
	svc, repo := newSummarySvc(t)
	id := seedSummaryExam(t, repo, nil)
	_, err := svc.DeleteSummary(context.Background(), id, "  ",
		SummaryAuthor{Type: domain.UploadedByParent, ID: 1, Name: "妈妈"})
	require.True(t, errors.Is(err, domain.ErrInvalidInput))
}

func TestSummaryHasContent(t *testing.T) {
	require.False(t, summaryHasContent(""))
	require.False(t, summaryHasContent("<p></p>"))
	require.False(t, summaryHasContent("<p>&nbsp;</p>"))
	require.False(t, summaryHasContent("   \n  "))
	require.True(t, summaryHasContent("<p>hi</p>"))
	require.True(t, summaryHasContent("<p><strong>a</strong></p>"))
}

// TestAppendSummaryConcurrentKeepsBothSegments proves two simultaneous appends
// both land: the conditional write retries on mismatch instead of the last
// write silently dropping the first segment (lost update).
func TestAppendSummaryConcurrentKeepsBothSegments(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := repository.Open(ctx, filepath.Join(dir, "paperly.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	examRepo := repository.NewExamRepoSQLite(db)
	svc := NewExamService(examRepo, repository.NewPageRepoSQLite(db), dir, nil).WithTxManager(db)
	id, err := examRepo.Insert(ctx, &domain.Exam{Status: domain.ExamStatusActive, UploadedBy: domain.UploadedByParent})
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	bodies := []string{"<p>alpha</p>", "<p>beta</p>"}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = svc.AppendSummary(ctx, id,
				SummaryAuthor{Type: domain.UploadedByParent, ID: int64(i + 1), Name: "parent"},
				bodies[i], summaryNow)
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	stored, err := examRepo.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, stored.Summary)
	require.Contains(t, *stored.Summary, "<p>alpha</p>")
	require.Contains(t, *stored.Summary, "<p>beta</p>")
}
