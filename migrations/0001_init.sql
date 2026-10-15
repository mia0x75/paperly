-- migrations/0001_init.sql
-- Paperly Box initial schema v1.0.0 (16 tables).
-- DDL aligned verbatim with DATABASE_DESIGN.md chapters 3/4.
-- Pre-release single-script idempotency: migrate re-runs 0001 every startup (all IF NOT EXISTS).


-- ────── users ──────
CREATE TABLE IF NOT EXISTS users (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    display_name TEXT NOT NULL UNIQUE,
    pin_hash     TEXT NOT NULL,
    pin_version  INTEGER NOT NULL DEFAULT 1,
    status       INTEGER NOT NULL DEFAULT 1,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- ────── kids ──────
CREATE TABLE IF NOT EXISTS kids (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    grade        INTEGER NOT NULL,
    gender       INTEGER,
    avatar       TEXT,
    access_code  TEXT NOT NULL UNIQUE,
    pin_hash     TEXT NOT NULL,
    pin_version  INTEGER NOT NULL DEFAULT 1,
    status       INTEGER NOT NULL DEFAULT 1,
    track        INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (grade BETWEEN 1 AND 12),
    CHECK (gender IS NULL OR gender IN (0, 1)),
    CHECK (status IN (1, 2, 3)),
    CHECK (track IN (0, 1, 2))
);

CREATE INDEX IF NOT EXISTS idx_kids_grade ON kids(grade);
-- Name is unique across the whole table (including soft-deleted/graduated). A named unique
-- index instead of an inline UNIQUE: CREATE TABLE IF NOT EXISTS never rewrites an existing DB,
-- so a standalone index statement applies to both new and upgraded DBs (idempotent; the script re-runs each startup).
CREATE UNIQUE INDEX IF NOT EXISTS idx_kids_name ON kids(name);


-- ────── subjects ──────
CREATE TABLE IF NOT EXISTS subjects (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    source      INTEGER NOT NULL DEFAULT 1,
    mask        INTEGER NOT NULL DEFAULT 31,
    mandatory   INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_subjects_sort ON subjects(sort_order);
CREATE INDEX IF NOT EXISTS idx_subjects_source ON subjects(source);


-- ────── kid_subjects ──────
-- Per-kid subject whitelist. A row means "this kid studies this subject"; no row
-- for a kid means "not configured" and subject filtering falls back to subjects.mask
-- by grade. subject_id is RESTRICT so a subject a kid still references cannot be
-- deleted (protects the whitelist); kid_id cascades so removing a kid drops its rows.
CREATE TABLE IF NOT EXISTS kid_subjects (
    kid_id       INTEGER NOT NULL REFERENCES kids(id) ON DELETE CASCADE,
    subject_id   INTEGER NOT NULL REFERENCES subjects(id) ON DELETE RESTRICT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (kid_id, subject_id)
);

CREATE INDEX IF NOT EXISTS idx_kid_subjects_subject ON kid_subjects(subject_id);


-- ────── migrations ──────
CREATE TABLE IF NOT EXISTS migrations (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    version      INTEGER NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    display_tag  TEXT,
    applied_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- ────── metrics ──────
CREATE TABLE IF NOT EXISTS metrics (
    -- SQLite has no native DATE type; metric_date / updated_at store ISO8601,
    -- consistent with the DB-wide DATETIME convention (DATETIME in SQLite is TEXT-affinity).
    metric_name TEXT NOT NULL,
    metric_date DATETIME NOT NULL,
    count       INTEGER NOT NULL DEFAULT 0,
    is_gauge    INTEGER NOT NULL DEFAULT 0,
    updated_at  DATETIME NOT NULL,
    PRIMARY KEY (metric_name, metric_date)
);

CREATE INDEX IF NOT EXISTS idx_metrics_date ON metrics(metric_date);


-- ────── cursors ──────
CREATE TABLE IF NOT EXISTS cursors (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_name    TEXT NOT NULL UNIQUE,
    last_id      INTEGER NOT NULL DEFAULT 0,
    last_run_at  DATETIME,
    last_status  INTEGER NOT NULL DEFAULT 0,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- ────── tasks ──────
CREATE TABLE IF NOT EXISTS tasks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         INTEGER NOT NULL,
    priority     INTEGER NOT NULL DEFAULT 50,
    status       INTEGER NOT NULL DEFAULT 0,
    retry_count  INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    detail       TEXT NOT NULL DEFAULT '{}',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at   DATETIME,
    finished_at  DATETIME
);

CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(status, priority DESC, created_at ASC);


-- ────── exams ──────
CREATE TABLE IF NOT EXISTS exams (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    title          TEXT,
    subject_id     INTEGER REFERENCES subjects(id) ON DELETE SET NULL,
    grade          INTEGER,
    exam_type      INTEGER,
    exam_date      DATE,
    kid_id         INTEGER REFERENCES kids(id) ON DELETE SET NULL,
    uploaded_by    INTEGER NOT NULL,
    summary        TEXT,
    summary_source INTEGER,
    status         INTEGER NOT NULL DEFAULT 1,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (grade IS NULL OR grade BETWEEN 1 AND 12)
);

CREATE INDEX IF NOT EXISTS idx_exams_kid ON exams(kid_id);
CREATE INDEX IF NOT EXISTS idx_exams_subject ON exams(subject_id);
CREATE INDEX IF NOT EXISTS idx_exams_grade ON exams(grade);
CREATE INDEX IF NOT EXISTS idx_exams_exam_type ON exams(exam_type);
CREATE INDEX IF NOT EXISTS idx_exams_exam_date ON exams(exam_date);


-- ────── pages ──────
CREATE TABLE IF NOT EXISTS pages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    exam_id     INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    page_index  INTEGER NOT NULL,
    file_path   TEXT NOT NULL,
    sha256      TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exam_id, page_index)
);

CREATE INDEX IF NOT EXISTS idx_pages_exam ON pages(exam_id);


-- ────── knowledge_points ──────
CREATE TABLE IF NOT EXISTS knowledge_points (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    subject_id  INTEGER NOT NULL REFERENCES subjects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    source      INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(subject_id, name)
);

CREATE INDEX IF NOT EXISTS idx_kp_subject ON knowledge_points(subject_id);
CREATE INDEX IF NOT EXISTS idx_kp_sort ON knowledge_points(subject_id, sort_order);
CREATE INDEX IF NOT EXISTS idx_kp_source ON knowledge_points(source);


-- ────── questions ──────
CREATE TABLE IF NOT EXISTS questions (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    exam_id             INTEGER NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
    page_id             INTEGER REFERENCES pages(id) ON DELETE SET NULL,
    question_no         INTEGER NOT NULL,
    question_type       INTEGER NOT NULL,

    content             TEXT,
    image_path          TEXT,
    image_sha256        TEXT,
    student_answer      TEXT,
    correct_answer      TEXT,
    is_correct          INTEGER NOT NULL DEFAULT 0,

    content_source      INTEGER,
    answer_source       INTEGER,
    is_correct_source   INTEGER,

    review_status       INTEGER NOT NULL DEFAULT 0,
    reviewed_by         INTEGER,
    reviewed_by_type    INTEGER,
    reviewed_at         DATETIME,
    review_note         TEXT,

    status              INTEGER NOT NULL DEFAULT 1,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exam_id, question_no)
);

CREATE INDEX IF NOT EXISTS idx_questions_exam ON questions(exam_id);


-- ────── question_kps ──────
CREATE TABLE IF NOT EXISTS question_kps (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    question_id         INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    knowledge_point_id  INTEGER NOT NULL REFERENCES knowledge_points(id) ON DELETE CASCADE,
    is_primary          INTEGER NOT NULL DEFAULT 1,
    source              INTEGER NOT NULL DEFAULT 1,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(question_id, knowledge_point_id)
);

CREATE INDEX IF NOT EXISTS idx_qkp_question ON question_kps(question_id);
CREATE INDEX IF NOT EXISTS idx_qkp_kp ON question_kps(knowledge_point_id);


-- ────── levels ──────
CREATE TABLE IF NOT EXISTS levels (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    kid_id              INTEGER NOT NULL REFERENCES kids(id) ON DELETE CASCADE,
    knowledge_point_id  INTEGER NOT NULL REFERENCES knowledge_points(id) ON DELETE CASCADE,
    correct_count       INTEGER NOT NULL DEFAULT 0,
    wrong_count         INTEGER NOT NULL DEFAULT 0,
    recent_wrong_count  INTEGER NOT NULL DEFAULT 0,
    level               INTEGER NOT NULL DEFAULT 0,
    first_practiced_at  DATETIME,
    last_practiced_at   DATETIME,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(kid_id, knowledge_point_id)
);

CREATE INDEX IF NOT EXISTS idx_levels_kid_level ON levels(kid_id, level);


-- ────── gaps ──────
CREATE TABLE IF NOT EXISTS gaps (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    kid_id              INTEGER NOT NULL REFERENCES kids(id) ON DELETE CASCADE,
    knowledge_point_id  INTEGER NOT NULL REFERENCES knowledge_points(id) ON DELETE CASCADE,
    severity            INTEGER NOT NULL DEFAULT 1,
    error_count         INTEGER NOT NULL DEFAULT 1,
    latest_question_id  INTEGER REFERENCES questions(id) ON DELETE SET NULL,
    reason_hint         TEXT,
    status              INTEGER NOT NULL DEFAULT 1,
    resolved_at         DATETIME,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gaps_kid_status ON gaps(kid_id, status);
CREATE INDEX IF NOT EXISTS idx_gaps_kid_kp ON gaps(kid_id, knowledge_point_id);

-- Partial unique index: at most one active gap per kid per knowledge point
CREATE UNIQUE INDEX IF NOT EXISTS uq_gaps_active_kid_kp
    ON gaps(kid_id, knowledge_point_id)
    WHERE status = 1;


-- ────── messages ──────
CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    audience    INTEGER NOT NULL,
    kid_id      INTEGER REFERENCES kids(id) ON DELETE CASCADE,
    kind        INTEGER NOT NULL,
    title       TEXT NOT NULL,
    body        TEXT,
    link        TEXT,
    is_read     INTEGER NOT NULL DEFAULT 0,
    read_at     DATETIME,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- kid_id is required when audience=2 (kid) and must be NULL when audience=1 (parent)
    CHECK (
        (audience = 1 AND kid_id IS NULL) OR
        (audience = 2 AND kid_id IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_messages_parent ON messages(is_read, created_at DESC)
    WHERE audience = 1;
CREATE INDEX IF NOT EXISTS idx_messages_kid ON messages(kid_id, is_read, created_at DESC)
    WHERE audience = 2;


-- Baseline version record (idempotent) --
INSERT OR IGNORE INTO migrations (version, name, display_tag)
VALUES (1, '0001_init.sql', 'v1.0.0');

UPDATE migrations SET display_tag = 'v1.0.0', name = '0001_init.sql' WHERE version = 1;
