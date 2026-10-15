import { apiDelete, apiGet, apiPost, apiPut } from "./client";
import type {
  CreateKidReq,
  UpdateKidReq,
  ResetAccessCodeResponse,
  ResetPinResponse,
  KidCreated,
  KidDetail,
  KidSummary,
  SubjectSummary,
} from "./types";

/** Kid API: list / detail / create / update / delete. */
export const kidApi = {
  /** Status omitted = deleted excluded; include_deleted=true returns them too (settings restore). */
  list(params?: { status?: number; grade?: number; include_deleted?: boolean }): Promise<KidSummary[]> {
    return apiGet<KidSummary[]>("/kids", params);
  },
  get(id: number): Promise<KidDetail> {
    return apiGet<KidDetail>(`/kids/${id}`);
  },
  create(req: CreateKidReq): Promise<KidCreated> {
    return apiPost<KidCreated>("/kids", req);
  },
  update(id: number, req: UpdateKidReq): Promise<KidDetail> {
    return apiPut<KidDetail>(`/kids/${id}`, req);
  },
  remove(id: number): Promise<void> {
    return apiDelete<void>(`/kids/${id}`);
  },
  /** Restore a soft-deleted kid (status: deleted -> active). */
  restore(id: number): Promise<KidDetail> {
    return apiPost<KidDetail>(`/kids/${id}/restore`);
  },
  /** Reset the access code: old URL invalid immediately, new code returned only once. */
  resetAccessCode(id: number): Promise<ResetAccessCodeResponse> {
    return apiPost<ResetAccessCodeResponse>(`/kids/${id}/access_code/reset`);
  },
  /** Reset PIN: empty/omitted new_pin = clear; non-empty = set a new PIN. */
  resetPin(id: number, req: { new_pin?: string }): Promise<ResetPinResponse> {
    return apiPost<ResetPinResponse>(`/kids/${id}/pin/reset`, req);
  },
  /** The kid's subject whitelist (empty when unconfigured). */
  subjects(id: number): Promise<SubjectSummary[]> {
    return apiGet<SubjectSummary[]>(`/kids/${id}/subjects`);
  },
};
