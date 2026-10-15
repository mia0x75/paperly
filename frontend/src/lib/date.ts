/**
 * Date utilities: the backend always returns UTC "YYYY-MM-DD"; the frontend
 * handles presentation and filter params.
 *
 * Never mix `new Date("YYYY-MM-DD")` (UTC midnight) with `new Date(y, m, d)` (local
 * midnight): mixing shifts the "archive time" column by a whole day — a bug class
 * that never reproduces in test environments (usually UTC+).
 */

/** "YYYY-MM-DD" at local midnight, for formatting/sorting. */
export function parseDateOnly(value: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(value);
  if (!m) return null;
  const [, y, mth, d] = m;
  return new Date(Number(y), Number(mth) - 1, Number(d));
}

/** Date -> "YYYY-MM-DD" (local calendar day). For input[type=date] and query params. */
export function toDateOnly(value: Date): string {
  const y = value.getFullYear();
  const m = String(value.getMonth() + 1).padStart(2, "0");
  const d = String(value.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

/**
 * formatDateTime -> "YYYY-MM-DD HH:mm:ss" (local timezone), for full timestamps like
 * the detail "entry time". Unparseable returns the input unchanged (never throws);
 * empty returns "" for the caller's "—" fallback. No date library: one formatter
 * can't justify the dependency.
 */
export function formatDateTime(value: string | null | undefined): string {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/** Percent change; null when prev=0. */
export function deltaPercent(cur: number, prev: number): number | null {
  if (prev === 0) return null;
  return ((cur - prev) / prev) * 100;
}
