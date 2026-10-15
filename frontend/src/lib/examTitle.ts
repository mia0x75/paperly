import { gradeLabel } from "./grade";
import { parseDateOnly } from "./date";

/**
 * ExamType enum (matches `internal/domain/exam.go`); integers for storage, here only
 * for title suffix + month rule.
 */
export const EXAM_TYPE = {
  weekly: 1,
  monthly: 2,
  midterm: 3,
  final: 4,
  mock: 5,
  special: 6,
  other: 99,
} as const;

/** Exam type -> Chinese suffix (unknown falls into "其他"). */
const EXAM_TYPE_LABEL: Record<number, string> = {
  [EXAM_TYPE.weekly]: "周测",
  [EXAM_TYPE.monthly]: "月考",
  [EXAM_TYPE.midterm]: "期中",
  [EXAM_TYPE.final]: "期末",
  [EXAM_TYPE.mock]: "模考",
  [EXAM_TYPE.special]: "专项",
  [EXAM_TYPE.other]: "其他",
};

/** Exam type dropdown options. */
export const EXAM_TYPE_OPTIONS: readonly { value: number; label: string }[] = [
  { value: EXAM_TYPE.weekly, label: "周测" },
  { value: EXAM_TYPE.monthly, label: "月考" },
  { value: EXAM_TYPE.midterm, label: "期中" },
  { value: EXAM_TYPE.final, label: "期末" },
  { value: EXAM_TYPE.mock, label: "模考" },
  { value: EXAM_TYPE.special, label: "专项" },
  { value: EXAM_TYPE.other, label: "其他" },
];

/** Only midterm/final omit the month (half-term nodes; month adds nothing). */
const NO_MONTH_TYPES: ReadonlySet<number> = new Set([EXAM_TYPE.midterm, EXAM_TYPE.final]);

/**
 * termOf returns the term: 9/1~1/31 上学期, 2/1~6/30 下学期. July~August (summer
 * break) returns 上学期 — it belongs to the upcoming September school year, and
 * labeling it 下学期 would confuse pre-enrollment exams with last term's.
 */
export function termOf(date: Date): "上学期" | "下学期" {
  const month = date.getMonth() + 1;
  // 7,8 -> 上学期 like 9..1 (see above)
  if (month >= 9 || month === 1 || month === 7 || month === 8) return "上学期";
  return "下学期";
}

/**
 * genTitle generates the exam title.
 *
 * Rules (finalized):
 *   - weekly/monthly/mock/special/other: `{yyyy}-{grade}-{term}-{subject}-{mm}-{type}`
 *   - midterm/final: `{yyyy}-{grade}-{term}-{subject}-{type}` (no month)
 *
 * Examples: 2026-09-25 + Math + monthly -> `2026-初三-上学期-数学-09月-月考`
 *           2026-11-10 + Math + midterm -> `2026-初三-上学期-数学-期中`
 *
 * Returns "" when any required item is missing: no pre-fill beats a broken "--数学-期中"
 * (harder to notice than empty).
 */
export function genTitle(
  examDate: string,
  grade: number,
  subjectName: string,
  examType: number,
): string {
  if (examDate === "" || subjectName === "") return "";
  const d = parseDateOnly(examDate);
  if (d === null) return "";

  const gradeText = gradeLabel(grade);
  if (gradeText === null) return "";

  const typeText = EXAM_TYPE_LABEL[examType];
  if (typeText === undefined) return "";

  const yyyy = String(d.getFullYear());
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const term = termOf(d);

  if (NO_MONTH_TYPES.has(examType)) {
    return `${yyyy}-${gradeText}-${term}-${subjectName}-${typeText}`;
  }
  return `${yyyy}-${gradeText}-${term}-${subjectName}-${mm}月-${typeText}`;
}
