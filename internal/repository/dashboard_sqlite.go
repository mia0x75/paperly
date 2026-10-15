package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"paperly/internal/domain"
)

// Read-only dashboard aggregations over read pool (WAL, multiple connections).
type DashboardRepoSQLite struct {
	baseRepo
}

var _ domain.KidDashboardRepository = (*DashboardRepoSQLite)(nil)

// NewDashboardRepoSQLite constructs the dashboard aggregation repository.
func NewDashboardRepoSQLite(db *DB) *DashboardRepoSQLite {
	return &DashboardRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that transaction.
func (r *DashboardRepoSQLite) WithTx(tx any) domain.KidDashboardRepository {
	return &DashboardRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

const tsLayout = "2006-01-02 15:04:05"

// countsQuery uses conditional aggregation (CASE WHEN) to keep exam and page counts
// on the same snapshot. Pages are fetched via scalar subquery (avoids expanding rows
// from JOINs that would affect COUNT(*)).
const countsQuery = `SELECT
		COUNT(*) AS exam_total,
		COUNT(CASE WHEN created_at >= ? AND created_at <= ? THEN 1 END) AS this_month,
		COUNT(CASE WHEN created_at >= ? AND created_at <= ? THEN 1 END) AS last_month,
		(SELECT COUNT(*) FROM pages p JOIN exams e2 ON e2.id = p.exam_id
			WHERE e2.status != ?%s) AS pages
	FROM exams
	WHERE status != ?%s`

func (r *DashboardRepoSQLite) ScopeCounts(ctx context.Context, kidID *int64, w domain.DashboardWindow) (domain.ScopeCounts, error) {
	now := w.To.Format(tsLayout)
	monthFrom := w.ThisMonthFrom.Format(tsLayout)
	lastFrom := w.LastMonthFrom.Format(tsLayout)
	lastTo := w.LastMonthTo.Format(tsLayout)

	examKid, pageKid := "", ""
	args := []any{monthFrom, now, lastFrom, lastTo, domain.ExamStatusDeleted}
	if kidID != nil {
		examKid, pageKid = " AND kid_id = ?", " AND e2.kid_id = ?"
		args = append(args, *kidID)
	}
	args = append(args, domain.ExamStatusDeleted)
	if kidID != nil {
		args = append(args, *kidID)
	}

	var out struct {
		ExamTotal int `db:"exam_total"`
		ThisMonth int `db:"this_month"`
		LastMonth int `db:"last_month"`
		Pages     int `db:"pages"`
	}
	if err := r.readExt().GetContext(ctx, &out, fmt.Sprintf(countsQuery, pageKid, examKid), args...); err != nil {
		return domain.ScopeCounts{}, fmt.Errorf("dashboard counts: %w", err)
	}
	return domain.ScopeCounts{
		ExamTotal:     out.ExamTotal,
		ExamThisMonth: out.ThisMonth,
		ExamLastMonth: out.LastMonth,
		ExamPages:     out.Pages,
	}, nil
}

// substr(created_at,1,10) avoids SQLite date() timezone/format quirks.
const uploadSeriesForKidQuery = `SELECT substr(created_at, 1, 10) AS date, COUNT(*) AS cnt
	FROM exams
	WHERE kid_id = ? AND status != ? AND created_at >= ? AND created_at <= ?
	GROUP BY substr(created_at, 1, 10)
	ORDER BY date ASC`

func (r *DashboardRepoSQLite) UploadSeriesForKid(ctx context.Context, kidID int64, w domain.DashboardWindow) ([]domain.UploadPoint, error) {
	raw, err := r.scanSeries(ctx, uploadSeriesForKidQuery,
		kidID, domain.ExamStatusDeleted, w.From.Format(tsLayout), w.To.Format(tsLayout))
	if err != nil {
		return nil, fmt.Errorf("kid upload series: %w", err)
	}
	return raw, nil
}

// Batch fetch per-kid upload series to avoid N+1 queries.
func (r *DashboardRepoSQLite) UploadSeriesForKids(ctx context.Context, kidIDs []int64, w domain.DashboardWindow) ([]domain.KidUploadSeries, error) {
	if len(kidIDs) == 0 {
		return []domain.KidUploadSeries{}, nil
	}

	placeholders := strings.Repeat("?,", len(kidIDs))
	placeholders = placeholders[:len(placeholders)-1]
	q := `SELECT kid_id, substr(created_at, 1, 10) AS date, COUNT(*) AS cnt
		FROM exams
		WHERE kid_id IN (` + placeholders + `) AND status != ? AND created_at >= ? AND created_at <= ?
		GROUP BY kid_id, substr(created_at, 1, 10)
		ORDER BY date ASC`

	args := make([]any, 0, len(kidIDs)+3)
	for _, id := range kidIDs {
		args = append(args, id)
	}
	args = append(args, domain.ExamStatusDeleted, w.From.Format(tsLayout), w.To.Format(tsLayout))

	var raw []struct {
		KidID int64  `db:"kid_id"`
		Date  string `db:"date"`
		Cnt   int    `db:"cnt"`
	}
	if err := r.readExt().SelectContext(ctx, &raw, q, args...); err != nil {
		return nil, fmt.Errorf("kids upload series: %w", err)
	}

	byKid := make(map[int64][]domain.UploadPoint, len(kidIDs))
	for _, row := range raw {
		d, err := time.Parse(dateLayout, row.Date)
		if err != nil {
			continue
		}
		byKid[row.KidID] = append(byKid[row.KidID], domain.UploadPoint{Date: d, Count: row.Cnt})
	}
	out := make([]domain.KidUploadSeries, 0, len(kidIDs))
	for _, id := range kidIDs {
		if pts, ok := byKid[id]; ok {
			out = append(out, domain.KidUploadSeries{KidID: id, Points: pts})
		}
	}
	return out, nil
}

// Converts (date,cnt) rows; skips invalid dates to avoid failing the whole series.
func (r *DashboardRepoSQLite) scanSeries(ctx context.Context, q string, args ...any) ([]domain.UploadPoint, error) {
	var raw []struct {
		Date string `db:"date"`
		Cnt  int    `db:"cnt"`
	}
	if err := r.readExt().SelectContext(ctx, &raw, q, args...); err != nil {
		return nil, err
	}

	out := make([]domain.UploadPoint, 0, len(raw))
	for _, row := range raw {
		d, err := time.Parse(dateLayout, row.Date)
		if err != nil {
			continue
		}
		out = append(out, domain.UploadPoint{Date: d, Count: row.Cnt})
	}
	return out, nil
}

// Counts only subjects present in kid_subjects (no fallback).
const subjectDistributionForKidQuery = `SELECT
		s.id AS subject_id, s.name AS name, COUNT(e.id) AS cnt
	FROM exams e
	JOIN subjects s ON s.id = e.subject_id
	JOIN kid_subjects ks ON ks.subject_id = s.id
	WHERE e.kid_id = ?
	  AND e.status != ?
	  AND ks.kid_id = ?
	GROUP BY s.id, s.name
	ORDER BY cnt DESC, s.sort_order ASC`

func (r *DashboardRepoSQLite) SubjectDistributionForKid(ctx context.Context, kidID int64) ([]domain.SubjectDistribution, error) {
	out := make([]domain.SubjectDistribution, 0)
	if err := r.readExt().SelectContext(ctx, &out, subjectDistributionForKidQuery, kidID, domain.ExamStatusDeleted, kidID); err != nil {
		return nil, fmt.Errorf("kid subject distribution: %w", err)
	}
	return out, nil
}

const subjectDistributionAllQuery = `SELECT
		s.id AS subject_id, s.name AS name, COUNT(e.id) AS cnt
	FROM exams e
	JOIN subjects s ON s.id = e.subject_id
	WHERE e.status != ?
	GROUP BY s.id, s.name
	ORDER BY cnt DESC, s.sort_order ASC`

func (r *DashboardRepoSQLite) SubjectDistributionAll(ctx context.Context) ([]domain.SubjectDistribution, error) {
	out := make([]domain.SubjectDistribution, 0)
	if err := r.readExt().SelectContext(ctx, &out, subjectDistributionAllQuery, domain.ExamStatusDeleted); err != nil {
		return nil, fmt.Errorf("all subject distribution: %w", err)
	}
	return out, nil
}
