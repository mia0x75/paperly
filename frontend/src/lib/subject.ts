/**
 * Subject visibility (subjects.mask) helpers.
 *
 * mask is a 5-bit INTEGER mirroring Go's domain constants: low 3 = stage
 * (bit0 primary 1–6, bit1 middle 7–9, bit2 high 10–12); high 2 = track (bit3 arts,
 * bit4 science). Visible when stage AND track bits both match (zero track = no track
 * filter). Core subjects carry both track bits, no separate "综合" bit.
 *
 * Kept in lockstep with internal/domain/subject.go — the backend stores the mask
 * but never returns a per-grade verdict, so both sides must agree.
 */

/** Stage bits (values fixed by the DB; do not renumber). */
export const SUBJECT_STAGE = {
  PRIMARY: 1,
  JUNIOR: 2,
  SENIOR: 4,
} as const;

/** Track bits (high 2 bits of mask; values fixed by the DB; do not renumber). */
export const SUBJECT_TRACK = {
  ARTS: 8,
  SCIENCE: 16,
} as const;

/** Ordered for the stage editor and badges. */
export const SUBJECT_STAGE_OPTIONS: readonly { bit: number; label: string }[] = [
  { bit: SUBJECT_STAGE.PRIMARY, label: "小学" },
  { bit: SUBJECT_STAGE.JUNIOR, label: "初中" },
  { bit: SUBJECT_STAGE.SENIOR, label: "高中" },
];

/** Ordered for the track editor and badges. */
export const SUBJECT_TRACK_OPTIONS: readonly { bit: number; label: string }[] = [
  { bit: SUBJECT_TRACK.ARTS, label: "文科" },
  { bit: SUBJECT_TRACK.SCIENCE, label: "理科" },
];

/** Grade from which track takes effect (senior high). */
export const TRACK_MIN_GRADE = 10;

/**
 * Grade 1–12 to stage bit; out-of-range yields 0 (matches nothing), mirroring backend StageBitForGrade.
 */
export function stageBitForGrade(grade: number | null | undefined): number {
  if (grade === null || grade === undefined || !Number.isInteger(grade)) return 0;
  if (grade >= 1 && grade <= 6) return SUBJECT_STAGE.PRIMARY;
  if (grade >= 7 && grade <= 9) return SUBJECT_STAGE.JUNIOR;
  if (grade >= 10 && grade <= 12) return SUBJECT_STAGE.SENIOR;
  return 0;
}

/**
 * trackBitFor maps track (0/1/2) to its mask bit, mirroring backend TrackBitFor.
 * Grade < 10 yields 0 regardless (split applies from senior high); track 0 yields 0.
 */
export function trackBitFor(track: number | null | undefined, grade?: number | null): number {
  if (grade !== undefined && grade !== null && grade < TRACK_MIN_GRADE) return 0;
  if (track === 1) return SUBJECT_TRACK.ARTS;
  if (track === 2) return SUBJECT_TRACK.SCIENCE;
  return 0;
}

/** Whether a subject shows for a grade (stage dimension only). */
export function subjectVisibleForGrade(
  subject: { mask?: number },
  grade: number | null | undefined,
): boolean {
  const bit = stageBitForGrade(grade);
  if (bit === 0) return true;
  if (subject.mask === undefined || !Number.isFinite(subject.mask)) return true;
  return (subject.mask & bit) !== 0;
}

/** Whether a subject shows for a track (track dimension only); grade < 10 or track 0 → no filtering. */
export function subjectVisibleForTrack(
  subject: { mask?: number },
  track: number | null | undefined,
  grade?: number | null,
): boolean {
  const bit = trackBitFor(track, grade);
  if (bit === 0) return true;
  if (subject.mask === undefined || !Number.isFinite(subject.mask)) return true;
  return (subject.mask & bit) !== 0;
}
