/** Shared display formatting for dashboard figures. */

/**
 * avgPagesPerExam renders "平均 X.X 页/份"; "" when total is 0 (0/0 has no ratio).
 */
export function avgPagesPerExam(pages: number, total: number): string {
  if (total === 0) return "";
  return `平均 ${(pages / total).toFixed(1)} 页/份`;
}
