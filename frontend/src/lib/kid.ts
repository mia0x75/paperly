import { KID_GENDER, KID_STATUS } from "../api/types";

/**
 * Display mapping for kid status / gender.
 *
 * `kids.status`/`gender` are INTEGER in the DB (see DATABASE_DESIGN.md); Chinese maps
 * only here, never stored nor in DTOs. Status: 1 active / 2 graduated / 3 soft-deleted;
 * gender: 1 male / 0 female / null unset.
 */

export type StatusVariant = "success" | "secondary" | "destructive";

/** Status -> short Chinese label (grade added by the caller). */
export function kidStatusLabel(status: number): string {
  if (status === KID_STATUS.GRADUATED) return "已毕业";
  if (status === KID_STATUS.DELETED) return "已删除";
  return "在读";
}

export function kidStatusVariant(status: number): StatusVariant {
  if (status === KID_STATUS.ACTIVE) return "success";
  if (status === KID_STATUS.DELETED) return "destructive";
  return "secondary";
}

export function genderLabel(gender: number | null): string {
  if (gender === KID_GENDER.MALE) return "男";
  if (gender === KID_GENDER.FEMALE) return "女";
  return "—";
}

/** Minimal id+name shape (instead of KidSummary) so callers pass store lists. */
export interface KidNameRef {
  id: number;
  name: string;
}

/**
 * kidNameById resolves exams.kid_id to a kid name.
 *
 * `kids.name` isn't in the exams DTO (only the FK), so rendering raw degrades to
 * `学生 #7`. Maps through the top bar's global list — no extra request. The `学生 #id`
 * fallback stays identifiable when the list isn't ready or the kid is gone (FK is
 * ON DELETE SET NULL, but history may point at a missing id).
 */
export function kidNameById(
  kids: readonly KidNameRef[],
  id: number | null | undefined,
): string {
  if (id === null || id === undefined) return "未归属";
  return kids.find((k) => k.id === id)?.name ?? `学生 #${id}`;
}
