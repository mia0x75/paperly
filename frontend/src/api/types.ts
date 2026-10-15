/**
 * Paperly API contract types (aligned with backend internal/api/dto).
 * Three-view principle: Summary (list) / Detail (detail) / Created (create response).
 */

// ---------- Kids ----------

/** Kid status (INTEGER): 1 active / 2 graduated / 3 soft-deleted. */
export const KID_STATUS = { ACTIVE: 1, GRADUATED: 2, DELETED: 3 } as const;

/** Kid gender (INTEGER): 1 male / 0 female / null unset. */
export const KID_GENDER = { MALE: 1, FEMALE: 0 } as const;

/** Kid arts/science track (INTEGER): 0 undecided / 1 arts / 2 science. Mirrors domain.KidTrack*. */
export const KID_TRACK = { UNDECIDED: 0, ARTS: 1, SCIENCE: 2 } as const;

export const KID_TRACK_OPTIONS: readonly { value: number; label: string }[] = [
  { value: KID_TRACK.UNDECIDED, label: "未分科" },
  { value: KID_TRACK.ARTS, label: "文科" },
  { value: KID_TRACK.SCIENCE, label: "理科" },
];

export interface KidSummary {
  id: number;
  name: string;
  /** Grade INTEGER 1~12 (DB CHECK constraint); UI label via gradeLabel(). */
  grade: number;
  /** 1 active / 2 graduated / 3 soft-deleted. */
  status: number;
  /** 0 undecided / 1 arts / 2 science; drives subject track filtering. */
  track: number;
}

export interface KidDetail {
  id: number;
  name: string;
  /** Grade INTEGER 1~12; UI label via gradeLabel(). */
  grade: number;
  /** 1 male / 0 female / null unset. */
  gender: number | null;
  avatar: string | null;
  /** 1 active / 2 graduated / 3 soft-deleted. */
  status: number;
  /** 0 undecided / 1 arts / 2 science. */
  track: number;
  created_at: string;
  updated_at: string;
}

export interface KidCreated {
  id: number;
  name: string;
  access_code: string;
  kid_url: string;
  created_at: string;
}

/** Reset access code response: the new code is returned only once — hand it to the parent immediately. */
export interface ResetAccessCodeResponse {
  kid_id: number;
  access_code: string;
  kid_url: string;
}

/** Reset PIN response: only reports whether a PIN is set, never echoes pin_hash. */
export interface ResetPinResponse {
  kid_id: number;
  pin_set: boolean;
}

/**
 * CreateKidReq is the payload for POST /api/kids.
 *
 * grade is **INTEGER 1~12** (DB CHECK constraint), not grade text:
 * grade is a structured column; Chinese labels only exist in the UI layer via gradeLabel().
 * The table also has no student-id / class / birthday / tags columns—do not send
 * these fields in the request; the backend createKidRequest does not know them
 * (and passing grade as a string returns 400 outright).
 */
export interface CreateKidReq {
  name: string;
  grade: number;
  /** 1 male / 0 female; omit when not set. */
  gender?: number;
  /** 4-6 digit PIN; omit to leave the invite flow to set it. */
  pin?: string;
  /**
   * The kid's subject whitelist (kid_subjects). When present (even empty) the
   * backend writes the profile and the subjects in one transaction; when omitted
   * only the profile is written. Mandatory subjects (语文 / 数学) are always
   * force-included by the backend, so a set missing them is repaired, not rejected.
   */
  subject_ids?: number[];
}

/**
 * UpdateKidReq is the payload for PUT /api/kids/:id (full-row PUT).
 * The backend accepts name/grade/gender/track; avatar is not changed through this path.
 * track is required (full-row PUT): 0 undecided / 1 arts / 2 science.
 */
export interface UpdateKidReq {
  name: string;
  grade: number;
  /** 1 male / 0 female; omitted = unset. */
  gender?: number;
  /** 0 undecided / 1 arts / 2 science. */
  track: number;
  /**
   * The kid's subject whitelist (kid_subjects), full replacement. Present opts into
   * the atomic profile+whitelist write; omitted leaves the whitelist untouched.
   */
  subject_ids?: number[];
}

// ---------- Exams ----------

/** Exam status (INTEGER, aligned with backend domain.ExamStatus*): 1 active / 2 soft-deleted. */
export const EXAM_STATUS = { ACTIVE: 1, DELETED: 2 } as const;

/**
 * ExamSummary is a GET /api/exams list item.
 *
 * The title is pre-filled by genTitle (editable); the summary is the exam digest
 * shown as the exam name in the list when a title reads better.
 */
export interface ExamSummary {
  id: number;
  /** Exam title (frontend pre-fills via genTitle, parent may edit). undefined when unnamed. */
  title?: string;
  /** Subject id; undefined when unlabeled (corresponds to exams.subject_id IS NULL). */
  subject_id?: number;
  /** Grade INTEGER 1~12. */
  grade?: number;
  /** Exam type enum INTEGER (1 weekly / 2 monthly / ...). */
  exam_type?: number;
  /** Exam date "YYYY-MM-DD". */
  exam_date?: string;
  /** Owning kid; undefined = unassigned. */
  kid_id?: number;
  /** Uploader: 1 = parent, 2 = kid self-upload (audit field, read-only). */
  uploaded_by: number;
  status: number;
  /** Exam digest—the "exam name" shown in the list. */
  summary?: string;
  /** Digitized page count. */
  page_count: number;
}

export interface ExamDetail extends ExamSummary {
  summary_source?: number;
  /** Exam pages (detail page list, including page images needed for preview). */
  pages: PageSummary[];
  created_at: string;
  updated_at: string;
}

export interface PageSummary {
  id: number;
  page_index: number;
  sha256: string;
}

/**
 * PageOpsPayload is the `operations` field for POST /api/exams/:id/pages/ops
 * (a JSON string submitted via multipart form, see examApi.opsPages).
 *
 * final_order is the **complete page order** after saving: existing pages pass
 * their numeric id, new pages pass a placeholder `new_<index>`—index must be
 * exactly 0..newCount-1, matching the `file` fields submitted in order.
 * Pages in delete_page_ids must not appear in final_order.
 */
export interface PageOpsPayload {
  delete_page_ids: number[];
  final_order: (number | string)[];
}

/** PageOpsResponse is the 202 of the page-reorder endpoint: task queued; poll the detail. */
export interface PageOpsResponse {
  task_id: number;
  exam_id: number;
}

export interface ExamListResponse {
  items: ExamSummary[];
  /** Next-page cursor; 0 means no more (has_more is false in that case). */
  next_cursor: number;
  has_more: boolean;
}

/**
 * SummaryResponse is the response of POST /api/exams/:id/summary and
 * DELETE /api/exams/:id/summary/:sig.
 *
 * `sig` is returned only by append (the new segment id); `summary` is always the
 * full updated summary HTML, so callers patch the local detail with it.
 */
export interface SummaryResponse {
  sig?: string;
  summary: string;
}

/**
 * UpdateExamReq is the payload for PUT /api/exams/:id.
 *
 * All fields are **required**: the backend does a full-row PUT, where
 * subject/grade/exam_type/manual_notes as nil means clear and owner_kid_id as
 * nil means unassign. If callers could send only changed fields, the omitted
 * ones would be silently cleared—requiring all fields forces callers to send
 * complete state back.
 * owner_kid_id null means unassign (uploader uploader_* is immutable and not here).
 */
export interface UpdateExamReq {
  /** Exam title. null clears the title. */
  title: string | null;
  /** Structured column (no more subject text column). null clears. */
  subject_id: number | null;
  grade: number | null;
  exam_type: number | null;
  exam_date: string | null;
  /** Owning kid. null = unassign. Uploader uploaded_by is immutable. */
  kid_id: number | null;
  summary?: string | null;
}

export interface UploadExamResponse {
  id: number;
  title?: string;
  subject_id?: number;
  grade?: number;
  exam_type?: number;
  uploaded_by: number;
  status: number;
  created_at: string;
}

// ---------- Homepage aggregate metrics (GET /api/metrics/summary) ----------

/**
 * Storage card: assets usage and disk usage ratio are **two distinct facts**;
 * do not merge them in the presentation layer.
 * assets_bytes = parent-uploaded exams; disk_used_ratio = the whole disk.
 */
export interface StorageCard {
  assets_bytes: number;
  assets_human: string;
  disk_total: number;
  disk_free: number;
  disk_used_ratio: number;
  /** false when the platform does not support statfs (e.g. Windows); disk_used_ratio is meaningless then. */
  disk_supported: boolean;
}

/** Database card: main DB size + last backup time. */
export interface DatabaseCard {
  size_bytes: number;
  size_human: string;
  /** Empty string = never backed up (not the year 1970). */
  last_backup_at: string;
  schema_tag: string;
}

/** One row of the health card (a single check item in the health report). */
export interface HealthItem {
  key: string;
  title: string;
  status: string;
  value: string;
  detail: string;
}

/**
 * MetricsSummary is the aggregate response for the homepage truth cards.
 */
export interface MetricsSummary {
  database: DatabaseCard;
  storage: StorageCard;
  health: HealthItem[];
  checked_at: string;
}

// ---------- Storage growth trend (GET /api/metrics/storage-trend) ----------

/**
 * StorageTrendPoint is a single-day data point on the storage trend line.
 *
 * null = missing sample that day (daily maintenance did not run / sampling
 * failed): the frontend renders a **line break**, never fills 0—not sampling a
 * size does not mean the size is 0, and filling 0 would punch a false trough into the trend chart.
 */
export interface StorageTrendPoint {
  date: string;
  db_size_kb: number | null;
  assets_size_kb: number | null;
}

/** StorageTrend is the storage growth trend response (last `days` days, ascending by date, missing days omitted). */
export interface StorageTrend {
  days: number;
  points: StorageTrendPoint[];
}

// ---------- Dashboard archive aggregation (GET /api/dashboard) ----------

/**
 * ScopeCounts is a single-scope count for the first-row KPI cards (GET /api/dashboard/counts).
 *
 * Every count is single-scope: the caller picks the scope at request time (all / one
 * kid), so there is no kid/all split.
 */
export interface ScopeCounts {
  /** Total archived exams (card 1 main number). */
  exam_total: number;
  /** New this month (card 2 main number). */
  exam_this_month: number;
  /** Same scope last month (base period for card 2's "vs last month +12%"). */
  exam_last_month: number;
  /** Total digitized pages (card 3 main number). */
  exam_pages: number;
}

/** One date bucket of upload persistence (gap days already filled with 0 by the backend; length always equals the window days). */
export interface DailyUpload {
  date: string;
  count: number;
}

/**
 * KidPersistence is one kid's series in "kids' persistence" (parent-side ROW2).
 *
 * Parallel to Dashboard.series (single kid): it fetches all active kids at once,
 * grouped by kid into stacked line data. Zero-fill rules match the single-kid
 * case (gaps 0, length = window days).
 */
export interface KidPersistence {
  kid_id: number;
  name: string;
  series: DailyUpload[];
}

/** One row of subject distribution. percent is 0~100, one decimal. */
export interface SubjectShare {
  subject_id: number;
  name: string;
  count: number;
  percent: number;
}

/** Kid reference carried by dashboard (id + business key name + status). */
export interface KidRef {
  id: number;
  name: string;
  grade: number | null;
  status: string;
}

/**
 * Dashboard is the homepage aggregate response. Parent and kid sides share the
 * **same structure**: every count is single-scope, so no global payload leaks to
 * the kid side.
 *
 * The global-scope exam list is not here—that belongs to the exam-list page.
 */
export interface ExamCounts {
  /** Total archived exams (ROW1 card 1 main number). */
  total: number;
  /** New this month (ROW1 card 2 main number). */
  this_month: number;
  /**
   * Same scope last month (denominator of card 1's "vs last month +12%").
   *
   * Must be delivered by the backend: it is not a derived "total - this month"
   * value—the total also includes exams from earlier months, so subtracting in
   * the frontend would compute "this month vs earlier", which is meaningless.
   */
  last_month: number;
}

export interface Dashboard {
  exams: ExamCounts;
  /** Total digitized pages (ROW1 card 3 main number). */
  exam_pages: number;
  series: DailyUpload[];
  distribution: SubjectShare[];
  kid: KidRef;
}

// --- Notifications (messages) ---

/**
 * Notification type (domain.MessageKind*, DB INTEGER enum).
 *
 * MUST stay in lockstep with Go's domain/message.go constants: the backend
 * validates/normalizes nothing here, so a drift silently mislabels every notice.
 * Display values go through a mapping; do not let Chinese leak anywhere outside
 * the switch in code: kindLabel() is the single entry point.
 */
export const MESSAGE_KIND = {
  GRADE_ADVANCED: 1,
  BACKUP_DONE: 2,
  BACKUP_FAILED: 3,
  AI_REVIEW_PENDING: 4,
  STORAGE_CLEANED: 5,
  GAP_RESOLVED: 6,
  REVIEW_DUE: 7,
  GRADE_CHANGED: 8,
  GRADE9_REMINDER: 9,
  GRADE10_REMINDER: 10,
  GRADE7_REMINDER: 11,
  OTHER: 99,
} as const;

export type MessageKind = (typeof MESSAGE_KIND)[keyof typeof MESSAGE_KIND];

/** List item in the bell dropdown (MessageSummary). */
export interface MessageSummary {
  id: number;
  kind: number;
  title: string;
  /** In-app jump target ("" = not navigable); see kindLabel's peers in lib/message.ts. */
  link: string;
  is_read: boolean;
  /** RFC3339. Render via formatRelative(), do not slice the string directly. */
  created_at: string;
}

/** keyset cursor pagination response: next_cursor=0 <=> has_more=false. */
export interface MessageListResponse {
  items: MessageSummary[];
  next_cursor: number;
  has_more: boolean;
}

export interface MessageUnreadCountResponse {
  unread_count: number;
}

export interface MessageMarkAllReadResponse {
  updated: number;
}

// ---------- Settings page (/settings) ----------

/**
 * ParentAccount is the Summary view of a parent account (for list references).
 *
 * **Has no pin_hash field**; not even the field name is delivered: pin_set only
 * reports whether it is set.
 * The frontend must never obtain the hash; if it does, the backend contract is broken.
 * This type is used for the parent-management page account list (GET /api/users).
 */
export interface ParentAccount {
  id: number;
  display_name: string;
  pin_set: boolean;
  status: number;
}

/**
 * AccountSummary is an item of GET /api/auth/parent-accounts (anonymous login-page list).
 *
 * Difference from ParentAccount: **no status**—the service layer returns only
 * active accounts, so the frontend does not filter again; the login page only
 * needs "who is selectable + whether a PIN is set".
 */
export interface AccountSummary {
  id: number;
  display_name: string;
  pin_set: boolean;
}

// ---------- Parent login state ----------

/** MeParent is the response of GET /api/me (parent audience) and POST /api/me/rename. */
export interface MeParent {
  id: number;
  display_name: string;
}

/** ParentLoginResult is the response of POST /api/auth/login. */
export interface ParentLoginResult {
  token: string;
  user: MeParent;
}

/** ParentChangePinResult is the response of POST /api/me/pin: only a new JWT. */
export interface ParentChangePinResult {
  token: string;
}

// ---------- Kid login state ----------

/** MeKid is the profile view of GET /api/me (kid audience) and kid login/invite responses. */
export interface MeKid {
  id: number;
  name: string;
  grade: number;
  track: number;
}

/** KidAccount is an item of the GET /api/auth/kid-accounts response (kid login page selection list). */
export interface KidAccount {
  id: number;
  name: string;
}

/** KidLoginResult is the response of POST /api/kid/auth/login. */
export interface KidLoginResult {
  token: string;
  kid: MeKid;
}

/** InviteVerifyResult is the response of POST /api/kid/invite/verify: invalid code yields {valid:false} (HTTP 200). */
export interface InviteVerifyResult {
  valid: boolean;
  name?: string;
}

/** InviteSetupResult is the response of POST /api/kid/invite/setup: a successful PIN setup issues the kid JWT. */
export interface InviteSetupResult {
  token: string;
  kid: MeKid;
}

/**
 * SubjectSummary is a subject list item.
 *
 * source is required: the settings page uses it to tell "system preset (locked,
 * undeletable)" from "custom (deletable)".
 */
export interface SubjectSummary {
  id: number;
  code: string;
  name: string;
  sort_order: number;
  source: number;
  /** Visibility bitmask: low 3 bits stage (bit0=primary, bit1=junior, bit2=senior), high 2 bits track (bit3=arts, bit4=science). 0~31. */
  mask: number;
  /** 1 = mandatory (语文/数学): editor force-checks it, backend force-includes it. */
  mandatory: number;
}

/** KnowledgePointSummary is a knowledge point list item. */
export interface KnowledgePointSummary {
  id: number;
  subject_id: number;
  name: string;
  sort_order: number;
}

/** KnowledgePoint is the POST knowledge-point response (id + the name after persisting). */
export interface KnowledgePoint {
  id: number;
  name: string;
}

/** Task status maps 1:1 to backend tasks.status (domain enum, stored as English). */
export const TASK_STATUS = {
  pending: 0,
  running: 1,
  done: 2,
  failed: 3,
  skipped: 4,
  cancelled: 5,
} as const;

/** TASK_STATUS_TEXT is the display text for status (DB stores English only; Chinese lives only in the UI layer). */
export const TASK_STATUS_TEXT: Record<number, string> = {
  [TASK_STATUS.pending]: "待执行",
  [TASK_STATUS.running]: "执行中",
  [TASK_STATUS.done]: "已完成",
  [TASK_STATUS.failed]: "失败",
  [TASK_STATUS.skipped]: "已跳过",
  [TASK_STATUS.cancelled]: "已取消",
};

/** TASK_KIND_TEXT is the display text for task kind. */
export const TASK_KIND_TEXT: Record<number, string> = {
  1: "聚合诊断",
  2: "年级推进",
  3: "备份",
  4: "文件清理",
  5: "重排页",
  99: "其他",
};

/** TaskSummary is a task list item (backend TaskSummary; the list omits error and payload). */
export interface TaskSummary {
  id: number;
  kind: number;
  status: number;
  priority: number;
  retry_count: number;
  created_at: string;
}

/**
 * TaskDetail is a single task (GET /tasks/:id and retry responses):
 * all TaskSummary fields + error and payload. The list endpoint omits the last four.
 */
export interface TaskDetail extends TaskSummary {
  last_error: string;
  detail: string;
  started_at: string;
  finished_at: string;
}

/**
 * TaskListResponse is the task list response (keyset cursor pagination).
 *
 * counts is "counts aggregated by status", **not total**:
 * cursor pagination cannot get an exact total, so rather than a fake total it
 * provides counts that can actually be computed.
 */
export interface TaskListResponse {
  items: TaskSummary[];
  next_cursor: number;
  has_more: boolean;
  counts: Record<string, number>;
}

/** SchemaMigration is a record of one applied migration. */
export interface SchemaMigration {
  version: number;
  name: string;
  display_tag: string;
  applied_at: string;
}

/** TaskCursor is the persisted state of one task cursor. */
export interface TaskCursor {
  task_name: string;
  last_id: number;
  last_run_at: string;
  last_status: number;
}
