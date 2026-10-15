package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// Silent logger for tests: only side paths such as notice failures log, and those are not
// assertion points.
func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Sentinel for "notice cannot be written".
var errNotifyFailed = errors.New("notify write failed")

type failCall struct {
	lastError string
	retryable bool
}

// In-memory TaskStore: Claim pops the queue in order; Finish/Skip/Fail are recorded.
type fakeTaskStore struct {
	queue    []*domain.Task
	claimed  []int64
	finished []int64
	skipped  map[int64]string
	failed   map[int64]failCall
}

func (f *fakeTaskStore) Claim(_ context.Context) (*domain.Task, error) {
	if len(f.queue) == 0 {
		return nil, domain.ErrNotFound
	}
	t := f.queue[0]
	f.queue = f.queue[1:]
	f.claimed = append(f.claimed, t.ID)
	return t, nil
}

func (f *fakeTaskStore) Finish(_ context.Context, id int64) error {
	f.finished = append(f.finished, id)
	return nil
}

func (f *fakeTaskStore) Skip(_ context.Context, id int64, reason string) error {
	if f.skipped == nil {
		f.skipped = map[int64]string{}
	}
	f.skipped[id] = reason
	return nil
}

func (f *fakeTaskStore) Fail(_ context.Context, id int64, lastError string, retryable bool) error {
	if f.failed == nil {
		f.failed = map[int64]failCall{}
	}
	f.failed[id] = failCall{lastError: lastError, retryable: retryable}
	return nil
}

// In-memory KidStore: lookups by id; Update overwrites and Graduate records.
type fakeKidStore struct {
	kids      map[int64]*domain.Kid
	updates   []*domain.Kid
	graduated []int64
}

func (f *fakeKidStore) GetByID(_ context.Context, id int64) (*domain.Kid, error) {
	k, ok := f.kids[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return k, nil
}

func (f *fakeKidStore) Update(_ context.Context, kid *domain.Kid) error {
	f.updates = append(f.updates, kid)
	if f.kids != nil {
		f.kids[kid.ID] = kid
	}
	return nil
}

func (f *fakeKidStore) Graduate(_ context.Context, id int64) error {
	f.graduated = append(f.graduated, id)
	return nil
}

// In-memory BackupRunner.
type fakeBackup struct {
	err   error
	calls int
}

func (f *fakeBackup) CreateBackup(_ context.Context) error {
	f.calls++
	return f.err
}

type noticeCall struct {
	kind  int
	title string
	body  string
	link  string
}

// kidNoticeCall mirrors noticeCall plus the recipient kid_id, so kid notices are recorded
// separately from parent notices (existing assertions on fakeNotifier.calls stay untouched).
type kidNoticeCall struct {
	kidID int64
	kind  int
	title string
	body  string
	link  string
}

// In-memory Notifier.
type fakeNotifier struct {
	err      error
	calls    []noticeCall
	kidCalls []kidNoticeCall
}

func (f *fakeNotifier) CreateParentNotice(_ context.Context, kind int, title, body, link string) (int64, error) {
	f.calls = append(f.calls, noticeCall{kind: kind, title: title, body: body, link: link})
	return int64(len(f.calls)), f.err
}

func (f *fakeNotifier) CreateKidNotice(_ context.Context, kidID int64, kind int, title, body, link string) (int64, error) {
	f.kidCalls = append(f.kidCalls, kidNoticeCall{kidID: kidID, kind: kind, title: title, body: body, link: link})
	return int64(len(f.kidCalls)), f.err
}

// Minimal Deps: silent log, temp DataDir, fixed clock.
func testDeps() Deps {
	return Deps{
		Log:     discardLog(),
		DataDir: "/tmp/paperly-worker-test",
		Clock:   func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) },
	}
}

// Asserts err is a skip signal with a matching reason.
func requireSkipped(t require.TestingT, err error, reason string) {
	var sk *errSkipped
	require.ErrorAs(t, err, &sk)
	require.Equal(t, reason, sk.reason)
}

// Asserts err is a non-retryable signal.
func requireNonRetryable(t require.TestingT, err error) {
	var nre *errNonRetryable
	require.ErrorAs(t, err, &nre)
}
