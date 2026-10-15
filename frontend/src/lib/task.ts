/**
 * Task helpers: elapsed-time display for the async/background queue log viewer.
 *
 * Task timestamps are local "YYYY-MM-DD HH:mm:ss" (backend DTO); browser Date parsing
 * of that shape is unreliable (Safari rejects it), so parse field-by-field. Same rule
 * on both ends keeps the difference timezone-proof.
 */

/** Parse "YYYY-MM-DD HH:mm:ss" into local epoch millis; null when malformed. */
function parseTaskTime(value: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})$/.exec(value);
  if (!m) return null;
  const [, y, mo, d, h, mi, s] = m;
  return new Date(Number(y), Number(mo) - 1, Number(d), Number(h), Number(mi), Number(s)).getTime();
}

/**
 * taskDurationText renders elapsed start->finish: <60s "5s", <1h "1m23s", else "1h02m".
 *
 * Precision: backend timestamps are whole seconds (see dto.formatTaskTime), so 1s is the
 * finest resolvable unit. "" when missing/malformed/negative — caller omits the parenthetical.
 */
export function taskDurationText(startedAt: string, finishedAt: string): string {
  if (startedAt === "" || finishedAt === "") return "";
  const start = parseTaskTime(startedAt);
  const end = parseTaskTime(finishedAt);
  if (start === null || end === null) return "";
  const total = Math.round((end - start) / 1000);
  if (total < 0) return "";
  if (total < 60) return `${total}s`;

  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  if (minutes < 60) return `${minutes}m${String(seconds).padStart(2, "0")}s`;

  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  return `${hours}h${String(remMinutes).padStart(2, "0")}m`;
}
