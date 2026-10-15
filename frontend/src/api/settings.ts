import { apiDelete, apiGet, apiPost, apiPut } from "./client";
import type {
  KnowledgePoint,
  KnowledgePointSummary,
  ParentAccount,
  SchemaMigration,
  SubjectSummary,
  TaskCursor,
  TaskDetail,
  TaskListResponse,
} from "./types";

/**
 * Settings page API aggregation module.
 *
 * One file because every section maps to one table — the endpoint-per-section mapping
 * reads at a glance. Kid calls still go through kidApi (shared types/paths), referenced
 * here only where reload semantics are needed.
 */
export const settingsApi = {
  // Parent accounts (users table). List/create go through unauthenticated /api/users*;
  // self-management uses Bearer /api/me* (see api/auth.ts).
  users(): Promise<ParentAccount[]> {
    return apiGet<ParentAccount[]>("/users");
  },
  createUser(displayName: string, pin?: string): Promise<ParentAccount> {
    return apiPost<ParentAccount>("/users", { display_name: displayName, pin });
  },

  // Subjects. No grade param: the manager shows the whole dictionary for toggling.
  subjects(): Promise<SubjectSummary[]> {
    return apiGet<SubjectSummary[]>("/subjects");
  },
  createSubject(name: string): Promise<{ id: number; name: string }> {
    return apiPost<{ id: number; name: string }>("/subjects", { name });
  },
  renameSubject(id: number, name: string): Promise<SubjectSummary> {
    return apiPut<SubjectSummary>(`/subjects/${id}`, { name });
  },
  updateSubjectMask(id: number, mask: number): Promise<SubjectSummary> {
    return apiPut<SubjectSummary>(`/subjects/${id}`, { mask });
  },
  deleteSubject(id: number): Promise<void> {
    return apiDelete<void>(`/subjects/${id}`);
  },

  // Knowledge points.
  knowledgePoints(subjectId: number): Promise<KnowledgePointSummary[]> {
    return apiGet<KnowledgePointSummary[]>(`/subjects/${subjectId}/knowledge_points`);
  },
  createKnowledgePoint(subjectId: number, name: string): Promise<KnowledgePoint> {
    return apiPost<KnowledgePoint>(`/subjects/${subjectId}/knowledge_points`, { name });
  },
  renameKnowledgePoint(subjectId: number, id: number, name: string): Promise<void> {
    return apiPut<void>(`/subjects/${subjectId}/knowledge_points/${id}`, { name });
  },
  deleteKnowledgePoint(subjectId: number, id: number): Promise<void> {
    return apiDelete<void>(`/subjects/${subjectId}/knowledge_points/${id}`);
  },

  // Background tasks.
  tasks(params?: { status?: number; cursor?: number; limit?: number }): Promise<TaskListResponse> {
    return apiGet<TaskListResponse>("/tasks", params);
  },
  task(id: number): Promise<TaskDetail> {
    return apiGet<TaskDetail>(`/tasks/${id}`);
  },
  retryTask(id: number): Promise<TaskDetail> {
    return apiPost<TaskDetail>(`/tasks/${id}/retry`);
  },
  clearFinishedTasks(): Promise<{ deleted: number }> {
    return apiPost<{ deleted: number }>("/tasks/clear-finished");
  },

  // Ops (migrations / cursors / backups).
  migrations(): Promise<SchemaMigration[]> {
    return apiGet<SchemaMigration[]>("/admin/migrations");
  },
  cursors(): Promise<TaskCursor[]> {
    return apiGet<TaskCursor[]>("/admin/cursors");
  },
  backups(): Promise<BackupList> {
    return apiGet<BackupList>("/admin/backups");
  },
  createBackup(): Promise<{ file_name: string; bytes: number }> {
    return apiPost<{ file_name: string; bytes: number }>("/admin/backup");
  },

  // Image settings.
  imageSettings(): Promise<ImageSettings> {
    return apiGet<ImageSettings>("/settings/image");
  },
  updateImageSettings(compressPreset: number): Promise<ImageSettings> {
    return apiPut<ImageSettings>("/settings/image", { compress_preset: compressPreset });
  },
};

export interface ImageSettings {
  compress_preset: number;
}

/** Display text for the five compression tiers (backend returns only a number). */
export const WEBP_PRESET_OPTIONS: readonly { value: number; label: string; hint: string }[] = [
  { value: 1, label: "无损保真", hint: "文件最大，画质无损" },
  { value: 2, label: "有损高品质", hint: "文字清晰，体积较大" },
  { value: 3, label: "有损均衡", hint: "推荐，体积与画质平衡" },
  { value: 4, label: "有损省空间", hint: "可能损失细小文字" },
  { value: 5, label: "有损极限压缩", hint: "不推荐，可能影响文字辨识" },
];

export interface BackupItem {
  timestamp: string;
  tier: string;
  file_name: string;
  bytes: number;
  integrity: string;
  created_at: string;
}

export interface BackupList {
  items: BackupItem[];
  total: number;
  last_backup_at: string;
  has_backup: boolean;
}

/**
 * backupDownloadURL assembles a direct backup download link.
 *
 * Deliberately <a download>, not fetch + Blob: a several-hundred-MB package in memory
 * can crash the tab on a 4GB Box; native download skips the JS heap.
 */
export function backupDownloadURL(fileName: string): string {
  return `/api/admin/backups/${encodeURIComponent(fileName)}/download`;
}