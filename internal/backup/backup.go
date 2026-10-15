// Package backup implements online SQLite backup.
//
// Core convention: backup uses `VACUUM INTO` — an atomic operation that
// produces a consistent snapshot without downtime or blocking reads (it waits
// for in-flight write transactions to finish). It does not use the
// `db.Close()`-style "stop and copy" approach, nor filesystem cp (cp would
// copy the torn WAL half-state).
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"paperly/internal/domain"
)

// Retention policy: daily 7 / weekly 4 / monthly 12.
const (
	DailyKeep   = 7
	WeeklyKeep  = 4
	MonthlyKeep = 12
)

// backupTimestampLayout is shared by backup file names and tier directories.
const backupTimestampLayout = "20060102_150405"

const backupFilePrefix = "paperly_"

type tier string

const (
	tierDaily   tier = "daily"
	tierWeekly  tier = "weekly"
	tierMonthly tier = "monthly"
)

var retentionCounts = map[tier]int{
	tierDaily:   DailyKeep,
	tierWeekly:  WeeklyKeep,
	tierMonthly: MonthlyKeep,
}

// tierOrder determines directory enumeration order, daily first (the tier with
// the most retained copies).
var tierOrder = []tier{tierDaily, tierWeekly, tierMonthly}

// Manifest exists so that a restore drill need not guess "which file is for
// which day"; it also makes cross-checking sha256 convenient.
type Manifest struct {
	Timestamp   string `json:"timestamp"`
	Tier        string `json:"tier"`
	DBFile      string `json:"db_file"`
	DBBytes     int64  `json:"db_bytes"`
	DBIntegrity string `json:"db_integrity"`
	AssetsBytes int64  `json:"assets_bytes"`
	CreatedAt   string `json:"created_at"`
}

// Info is a backup list item (API view, without internal absolute paths — only
// file names).
type Info struct {
	Timestamp string `json:"timestamp"`
	Tier      string `json:"tier"`
	FileName  string `json:"file_name"`
	Bytes     int64  `json:"bytes"`
	Integrity string `json:"integrity"`
	CreatedAt string `json:"created_at"`
}

type Service struct {
	opsRepo domain.OpsRepository
	metrics domain.SystemMetricsRepository
	dataDir string
	nowFn   func() time.Time
}

// dataDir is the data root; backups are written under <dataDir>/backup/.
func NewService(opsRepo domain.OpsRepository, metrics domain.SystemMetricsRepository, dataDir string) *Service {
	return &Service{
		opsRepo: opsRepo,
		metrics: metrics,
		dataDir: dataDir,
		nowFn:   time.Now,
	}
}

func (s *Service) backupRoot() string { return filepath.Join(s.dataDir, "backup") }

// Create runs a full backup: VACUUM INTO -> consistency check -> filesystem
// backup -> manifest -> retention policy.
//
// On failure it records backup_fail and deletes any generated half-done
// artifacts — leaving a bad file in the retention directory would eventually be
// used as a valid backup.
func (s *Service) Create(ctx context.Context) (*Manifest, int, error) {
	now := s.nowFn()
	ts := now.Format(backupTimestampLayout)
	t := tierFor(now)

	dir := filepath.Join(s.backupRoot(), string(t))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, 0, s.fail(ctx, fmt.Errorf("create backup dir: %w", err))
	}
	dbName := backupFilePrefix + ts + ".db"
	dbPath := filepath.Join(dir, dbName)

	if err := s.opsRepo.VacuumInto(ctx, dbPath); err != nil {
		return nil, 0, s.fail(ctx, err)
	}

	// Step 2: consistency check — open the backup file independently and run a
	// full integrity_check; a bad backup is discarded on the spot.
	integrity, err := s.opsRepo.VerifyFileIntegrity(ctx, dbPath)
	if err != nil {
		_ = os.Remove(dbPath)
		return nil, 0, s.fail(ctx, fmt.Errorf("verify backup integrity: %w", err))
	}
	if integrity != "ok" {
		_ = os.Remove(dbPath)
		return nil, 0, s.fail(ctx, fmt.Errorf("backup integrity check failed: %s", integrity))
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		_ = os.Remove(dbPath)
		return nil, 0, s.fail(ctx, fmt.Errorf("stat backup file: %w", err))
	}

	// After step 2, any failure must clear this run's artifacts.
	//
	// Reason: the backup directory is flat
	// (<prefix><ts>.db + .manifest.json + assets_<ts>/), so a leftover .db
	// without a manifest would be listed by List() as a valid backup, and the
	// parent would only find out later that the "latest backup" is unusable —
	// that is the real data loss. Better to void this whole backup than leave a
	// half-done that looks successful.
	abort := func(cause error) (*Manifest, int, error) {
		discardBackup(dir, ts)
		return nil, 0, s.fail(ctx, cause)
	}

	// Step 3: backup the filesystem (assets). The single-machine volume is
	// small, so the whole directory is copied directly.
	assetsBytes, err := copyDirInto(filepath.Join(s.dataDir, "assets"), dir, ts)
	if err != nil {
		return abort(fmt.Errorf("backup assets: %w", err))
	}

	m := &Manifest{
		Timestamp:   ts,
		Tier:        string(t),
		DBFile:      dbName,
		DBBytes:     info.Size(),
		DBIntegrity: integrity,
		AssetsBytes: assetsBytes,
		CreatedAt:   now.UTC().Format(time.RFC3339),
	}
	if err := s.writeManifest(dir, ts, m); err != nil {
		return abort(err)
	}

	pruned, err := s.applyRetention()
	if err != nil {
		// A retention failure does not affect the integrity of this backup: the
		// files are already complete, only the old ones were not pruned. Log the
		// fail so the health page can surface it, but do not mark an already
		// usable backup as failed.
		_ = s.metrics.Inc(ctx, domain.MetricBackupFail)
		_ = s.metrics.Inc(ctx, domain.MetricBackupSuccess)
		_ = s.metrics.SetMax(ctx, domain.MetricBackupLastAt, 1)
		return m, 0, nil
	}
	if pruned > 0 {
		_ = s.metrics.Inc(ctx, "backup_pruned")
	}

	// Success: backup_success +1, and write a backup_last_at row for the health
	// page to read MAX(updated_at).
	//
	// A failed metric write must not overturn the result: the backup file is on
	// disk and passed the integrity check; a 500 would make the parent think it
	// must rerun, and rerunning just produces another identical backup.
	// system_metrics is observability data whose loss does not affect data
	// safety — like the other metric writes in this package it is handled
	// best-effort.
	_ = s.metrics.Inc(ctx, domain.MetricBackupSuccess)
	_ = s.metrics.SetMax(ctx, domain.MetricBackupLastAt, 1)
	return m, pruned, nil
}

// discardBackup clears all artifacts of one incomplete backup. It only deletes
// files belonging to this run's timestamp and does not touch other backups in
// the directory.
func discardBackup(dir, ts string) {
	_ = os.Remove(filepath.Join(dir, backupFilePrefix+ts+".db"))
	_ = os.Remove(filepath.Join(dir, backupFilePrefix+ts+".manifest.json"))
	_ = os.RemoveAll(filepath.Join(dir, copiedDirName("assets", ts)))
}

func (s *Service) fail(ctx context.Context, err error) error {
	// A failed metric write must not override the main error: the backup has
	// already failed; failing to record the metric is a secondary problem.
	_ = s.metrics.Inc(ctx, domain.MetricBackupFail)
	return err
}

func (s *Service) writeManifest(dir, ts string, m *Manifest) error {
	blob, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	path := filepath.Join(dir, backupFilePrefix+ts+".manifest.json")
	if err := os.WriteFile(path, blob, 0o640); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// applyRetention enforces tiered retention: daily 7 / weekly 4 / monthly 12,
// returning the number of backups pruned.
//
// Idempotent: only timestamp ordering is considered; excess ones are deleted.
// Deleting also clears the manifest, to avoid orphan manifests pointing at
// deleted DBs.
func (s *Service) applyRetention() (int, error) {
	pruned := 0
	for _, t := range tierOrder {
		dir := filepath.Join(s.backupRoot(), string(t))
		names, err := listBackupFiles(dir)
		if err != nil {
			return pruned, err
		}
		keep := retentionCounts[t]
		if len(names) <= keep {
			continue
		}
		// names is in descending lexicographic (= timestamp) order, newest first.
		for _, name := range names[keep:] {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
				return pruned, fmt.Errorf("prune backup %s/%s: %w", t, name, err)
			}
			stem := name[len(backupFilePrefix) : len(name)-len(".db")]
			_ = os.Remove(filepath.Join(dir, backupFilePrefix+stem+".manifest.json"))
			_ = os.RemoveAll(filepath.Join(dir, "assets_"+stem))
			pruned++
		}
	}
	return pruned, nil
}

// listBackupFiles returns the .db file names in dir, newest first.
func listBackupFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // tier dir not created yet = no backups, not an error
		}
		return nil, fmt.Errorf("read backup dir %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) > len(backupFilePrefix)+3 &&
			n[:len(backupFilePrefix)] == backupFilePrefix && n[len(n)-3:] == ".db" {
			names = append(names, n)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// List serves GET /api/admin/backups: every tier, each in descending timestamp
// order.
func (s *Service) List() ([]Info, error) {
	var out []Info
	for _, t := range tierOrder {
		dir := filepath.Join(s.backupRoot(), string(t))
		names, err := listBackupFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			info := Info{Timestamp: name[len(backupFilePrefix) : len(name)-3], Tier: string(t), FileName: name}
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil {
				info.Bytes = st.Size()
			}
			if blob, err := os.ReadFile(filepath.Join(dir, backupFilePrefix+info.Timestamp+".manifest.json")); err == nil {
				var m Manifest
				if json.Unmarshal(blob, &m) == nil {
					info.Integrity = m.DBIntegrity
					info.CreatedAt = m.CreatedAt
				}
			}
			out = append(out, info)
		}
	}
	// Descending unified across tiers by timestamp — the parent cares about "the
	// most recent backups", not the tier.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out, nil
}

// LastBackupAt returns the most recent successful backup time (health page item
// 3; zero value when there is none).
func (s *Service) LastBackupAt(ctx context.Context) (time.Time, bool, error) {
	return s.metrics.LatestAt(ctx, domain.MetricBackupLastAt)
}

// tierFor assigns a backup to a tier: 1st of the month -> monthly, every Monday
// -> weekly, everything else -> daily.
//
// A backup can match several at once (e.g. Monday on the 1st); it then goes to
// monthly, which keeps the most copies, so weekly/monthly are never left empty
// just because they were "never assigned".
func tierFor(now time.Time) tier {
	switch {
	case now.Day() == 1:
		return tierMonthly
	case now.Weekday() == time.Monday:
		return tierWeekly
	default:
		return tierDaily
	}
}

// copiedDirName builds the file/folder name copied into the backup directory:
// <name>_<ts>.
//
// Its own function so that the backup artifact naming is exactly consistent
// with discardBackup's cleanup logic; otherwise a failed cleanup would miss a
// directory and leave a half-done behind.
func copiedDirName(name, ts string) string {
	return name + "_" + ts
}

// copyDirInto copies the src directory into dst/<name>_<ts> and returns the
// number of bytes copied.
//
// A missing source directory is normal (no exams uploaded yet) -> returns 0
// rather than an error.
func copyDirInto(src, dst, ts string) (int64, error) {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", src, err)
	}
	target := filepath.Join(dst, copiedDirName(filepath.Base(src), ts))
	if err := os.MkdirAll(target, 0o750); err != nil {
		return 0, fmt.Errorf("create %s: %w", target, err)
	}
	var total int64
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o750)
		}
		n, err := copyFileBytes(p, targetPath)
		if err != nil {
			return err
		}
		total += n
		return nil
	})
	if err != nil {
		return total, fmt.Errorf("copy dir %s: %w", src, err)
	}
	return total, nil
}
