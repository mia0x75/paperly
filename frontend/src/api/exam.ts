import { apiDelete, apiGet, apiPost, apiPut, apiUploadMany } from "./client";
import type {
  ExamDetail,
  ExamListResponse,
  PageOpsPayload,
  PageOpsResponse,
  SummaryResponse,
  UpdateExamReq,
  UploadExamResponse,
} from "./types";

/** Exam API: list / detail / upload / update / delete / page-level operations. */
export const examApi = {
  list(params?: {
    /** Cursor: returns only rows with id < cursor (id DESC). Omitted/0 = start from newest. */
    cursor?: number;
    /** Page size; backend default 20, max 100. */
    limit?: number;
    /** Subject id. */
    subject_id?: number;
    /** Grade INTEGER 1~12. */
    grade?: number;
    /** Exam type enum INTEGER. */
    exam_type?: number;
    /**
     * Status: 1 = normal / 2 = deleted. Omitted or 0 = all statuses, **including
     * deleted** (the exam list's default scope).
     */
    status?: number;
    /** Owning kid: only exams owned by that kid (homepage history table). */
    kid_id?: number;
    /** Exam date inclusive range, format YYYY-MM-DD. */
    date_from?: string;
    date_to?: string;
  }): Promise<ExamListResponse> {
    return apiGet<ExamListResponse>("/exams", params);
  },
  get(id: number): Promise<ExamDetail> {
    return apiGet<ExamDetail>(`/exams/${id}`);
  },
  uploadMany(
    files: readonly File[],
    fields?: {
      title?: string;
      subject_id?: number;
      grade?: number;
      exam_type?: number;
      exam_date?: string;
      summary?: string;
      /** Owning kid; omitted = unassigned. */
      kid_id?: number;
    },
  ): Promise<UploadExamResponse> {
    return apiUploadMany<UploadExamResponse>("/exams/upload", files, fields);
  },
  update(id: number, req: UpdateExamReq): Promise<ExamDetail> {
    return apiPut<ExamDetail>(`/exams/${id}`, req);
  },
  remove(id: number): Promise<void> {
    return apiDelete<void>(`/exams/${id}`);
  },
  /** Restore a soft-deleted exam (2 -> 1); calling on a normal exam 40012s. */
  restore(id: number): Promise<ExamDetail> {
    return apiPost<ExamDetail>(`/exams/${id}/restore`);
  },
  /**
   * Page-level operations (delete / add / drag-reorder; one reorder task per request).
   *
   * multipart: `operations` is a JSON PageOpsPayload string, `file` fields carry new
   * images in order (0 for pure delete/reorder). Returns the 202 task_id; then poll
   * the detail (uniform across sides; /api/tasks requires parent).
   */
  opsPages(
    id: number,
    payload: PageOpsPayload,
    files: readonly File[],
  ): Promise<PageOpsResponse> {
    return apiUploadMany<PageOpsResponse>(`/exams/${id}/pages/ops`, files, {
      operations: JSON.stringify(payload),
    });
  },
  /**
   * Append one signed summary segment (parent and kid may append). Returns the new sig
   * plus full updated HTML; patch local detail instead of re-fetching.
   */
  appendSummary(id: number, content: string): Promise<SummaryResponse> {
    return apiPost<SummaryResponse>(`/exams/${id}/summary`, { content });
  },
  /** Delete one signed summary segment (parent any; kid own only). */
  deleteSummary(id: number, sig: string): Promise<SummaryResponse> {
    return apiDelete<SummaryResponse>(`/exams/${id}/summary/${encodeURIComponent(sig)}`);
  },
};
