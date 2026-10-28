import { apiDelete, apiGet, apiPost } from "./client";
import type {
  MessageListResponse,
  MessageMarkAllReadResponse,
  MessageUnreadCountResponse,
} from "./types";

/**
 * messageApi — notification center.
 *
 * No kid_id parameter: the recipient boundary comes from the token audience
 * server-side; letting the client pass kid_id would hand back a
 * privilege-escalation entry.
 */
export const messageApi = {
  list(params?: { cursor?: number; limit?: number; unread_only?: boolean }): Promise<MessageListResponse> {
    return apiGet<MessageListResponse>("/messages", params);
  },
  /** Badge-only: one number, not the whole page. */
  unreadCount(): Promise<number> {
    return apiGet<MessageUnreadCountResponse>("/messages/unread-count").then((r) => r.unread_count);
  },
  markRead(id: number): Promise<void> {
    return apiPost<void>(`/messages/${id}/read`);
  },
  markAllRead(): Promise<number> {
    return apiPost<MessageMarkAllReadResponse>("/messages/read-all").then((r) => r.updated);
  },
  /** Physically remove one notification from the inbox (the server scopes it to the caller's own). */
  remove(id: number): Promise<void> {
    return apiDelete<void>(`/messages/${id}`);
  },
};