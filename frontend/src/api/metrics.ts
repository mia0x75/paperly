import { apiGet } from "./client";
import type { MetricsSummary, StorageTrend } from "./types";

/** Home dashboard aggregate metrics API: fetches the data-backed "ground-truth cards" in one request. */
export const metricsApi = {
  summary(): Promise<MetricsSummary> {
    return apiGet<MetricsSummary>("/metrics/summary");
  },
  /** storageTrend gauge snapshots of storage size over the last days days (ascending, missing days not backfilled). */
  storageTrend(days = 30): Promise<StorageTrend> {
    return apiGet<StorageTrend>("/metrics/storage-trend", { days });
  },
};
