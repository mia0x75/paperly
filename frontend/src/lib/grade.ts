/**
 * Grade mapping: `kids.grade` / `exams.grade` are **INTEGER 1~12** (DB CHECK); the UI
 * maps them to Chinese labels. Ranges: 1~6 primary, 7~9 junior, 10~12 senior.
 *
 * Mapping stays frontend-side: the same grade sorts/filters (exam-period comparison),
 * and stored Chinese would degrade sorting to pinyin order.
 */

/** Chinese label for 1~12 (index 0 unused; index = grade). */
const GRADE_LABELS: readonly string[] = [
  "", // 0 invalid
  "一年级",
  "二年级",
  "三年级",
  "四年级",
  "五年级",
  "六年级",
  "初一",
  "初二",
  "初三",
  "高一",
  "高二",
  "高三",
];

/**
 * Integer grade 1~12 to Chinese label; null/undefined/out-of-range -> null (caller renders "unset").
 */
export function gradeLabel(grade: number | null | undefined): string | null {
  if (grade === null || grade === undefined) return null;
  if (!Number.isInteger(grade) || grade < 1 || grade > 12) return null;
  return GRADE_LABELS[grade] ?? null;
}

/** Non-null gradeLabel ("未设置年级" when unset), for direct cell rendering. */
export function gradeLabelOr(grade: number | null | undefined, fallback = "未设置年级"): string {
  return gradeLabel(grade) ?? fallback;
}

/** Dropdown options for 1~12. */
export const GRADE_OPTIONS: readonly { value: number; label: string }[] = GRADE_LABELS.slice(1)
  .map((label, i) => ({ value: i + 1, label }))
  .filter((o) => o.label !== "");