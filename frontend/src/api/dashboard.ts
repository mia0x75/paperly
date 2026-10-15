import { apiGet } from "./client";
import type { Dashboard, KidPersistence, ScopeCounts, SubjectShare } from "./types";

/**
 * Dashboard archive aggregation API (GET /api/dashboard).
 * Separate from metricsApi: ops view (database/backup/disk/health) vs archive view
 * (pages/queue/persistence/distribution).
 */
export const dashboardApi = {
  /**
   * One kid's homepage aggregation. kidID required: 0/omitted makes the backend 400
   * rather than silently returning the whole family.
   */
  byKid(kidID: number, trendDays?: 7 | 30): Promise<Dashboard> {
    return apiGet<Dashboard>(
      "/dashboard",
      trendDays === undefined ? { kid_id: kidID } : { kid_id: kidID, trend_days: trendDays },
    );
  },

  /**
   * Kid-side homepage aggregation (GET /api/kid/dashboard). kid_id comes from the
   * JWT claim; the client can't specify it. Same Dashboard shape, single-scope counts.
   */
  kidOverview(trendDays?: 7 | 30): Promise<Dashboard> {
    return apiGet<Dashboard>(
      "/kid/dashboard",
      trendDays === undefined ? undefined : { trend_days: trendDays },
    );
  },

  /**
   * Parent-side "kids' persistence" (GET /api/dashboard/persistence): upload series
   * for all active kids, parallel to byKid's single line. Tolerate failure — fall back
   * to single-kid when an older backend lacks the endpoint.
   */
  persistence(trendDays?: 7 | 30): Promise<KidPersistence[]> {
    return apiGet<{ items: KidPersistence[] }>(
      "/dashboard/persistence",
      trendDays === undefined ? undefined : { trend_days: trendDays },
    ).then((r) => r.items);
  },

  /**
   * "Exam distribution" card data (GET /api/dashboard/distribution). kidID optional:
   * omitted = all kids (parent-homepage default). Separate from byKid so the card
   * switches filters without refetching the whole aggregation.
   */
  distribution(kidID?: number): Promise<SubjectShare[]> {
    return apiGet<{ items: SubjectShare[] }>(
      "/dashboard/distribution",
      kidID === undefined ? undefined : { kid_id: kidID },
    ).then((r) => r.items);
  },

  /**
   * Single-scope counts for the first KPI row (GET /api/dashboard/counts).
   *
   * kidID optional: omitted = **all kids** aggregation (the parent-homepage
   * default); a positive integer = narrow to that kid.
   * Lets each of the first three cards fetch its own filter on demand, kept
   * separate from byKid to avoid refetching the whole-page aggregation when switching filters.
   */
  scopeCounts(kidID?: number): Promise<ScopeCounts> {
    return apiGet<ScopeCounts>(
      "/dashboard/counts",
      kidID === undefined ? undefined : { kid_id: kidID },
    );
  },
};
