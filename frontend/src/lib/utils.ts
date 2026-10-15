import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * clsx handles conditional class names, tailwind-merge resolves conflicts (later wins).
 * Do not hand-concatenate class strings: conflicts fail silently into wrong styles.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
