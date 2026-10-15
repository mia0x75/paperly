import { MESSAGE_KIND, type MessageKind } from "../api/types";

/**
 * Display mapping for notification kinds (enums stored as-is, localized at display).
 * Unregistered kinds fall back to generic — one new enum must not blank the bell.
 */
const KIND_LABELS: Record<MessageKind, string> = {
  [MESSAGE_KIND.GRADE_ADVANCED]: "年级变更",
  [MESSAGE_KIND.BACKUP_DONE]: "备份完成",
  [MESSAGE_KIND.BACKUP_FAILED]: "备份失败",
  [MESSAGE_KIND.AI_REVIEW_PENDING]: "待人工复核",
  [MESSAGE_KIND.STORAGE_CLEANED]: "存储清理",
  [MESSAGE_KIND.GAP_RESOLVED]: "薄弱点攻克",
  [MESSAGE_KIND.REVIEW_DUE]: "待复习",
  [MESSAGE_KIND.GRADE_CHANGED]: "年级变更",
  [MESSAGE_KIND.GRADE9_REMINDER]: "初三科目提醒",
  [MESSAGE_KIND.GRADE10_REMINDER]: "高一分科提醒",
  [MESSAGE_KIND.GRADE7_REMINDER]: "初一科目提醒",
  [MESSAGE_KIND.OTHER]: "通知",
};

export function kindLabel(kind: number): string {
  return KIND_LABELS[kind as MessageKind] ?? KIND_LABELS[MESSAGE_KIND.OTHER];
}

/**
 * Notification timestamps: relative ("N 分钟/小时前") within 24h, calendar date beyond —
 * dense lists read better up close, dates locate better far out.
 */
export function formatRelative(value: string, now: Date = new Date()): string {
  const t = Date.parse(value);
  if (Number.isNaN(t)) return "";
  const diff = now.getTime() - t;
  // Clock skew may put created_at in the future: report "just now", not a negative span.
  if (diff < 0) return "刚刚";
  const min = Math.floor(diff / 60_000);
  if (min < 1) return "刚刚";
  if (min < 60) return `${min} 分钟前`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour} 小时前`;
  const d = new Date(t);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

function pad(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}
