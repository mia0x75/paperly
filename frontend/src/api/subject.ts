import { apiGet } from "./client";

/**
 * Subject is a dictionary entry (structured columns, not text tags).
 *
 * `mask` is the visibility bitmask (low 3: stage; high 2: track); `mandatory` can't
 * be dropped (语文/数学 = 1); `source` splits system presets (1) from parent-created (2).
 */
export interface Subject {
  id: number;
  code: string;
  name: string;
  sort_order: number;
  source: number;
  mask: number;
  mandatory: number;
}

/** Subject dictionary API. */
export const subjectApi = {
  /**
   * Lists subjects. grade (> 0) filters to that stage; kid_id returns exactly that
   * kid's kid_subjects (no mask fallback — unconfigured yields empty); omitting all
   * returns the full dictionary.
   */
  list(grade?: number, track?: number, kidId?: number): Promise<Subject[]> {
    const params = new URLSearchParams();
    if (kidId !== undefined && kidId > 0) params.set("kid_id", String(kidId));
    if (grade !== undefined && grade > 0) params.set("grade", String(grade));
    if (track !== undefined && track >= 0) params.set("track", String(track));
    const query = params.toString() ? `?${params.toString()}` : "";
    return apiGet<Subject[]>(`/subjects${query}`);
  },
};
