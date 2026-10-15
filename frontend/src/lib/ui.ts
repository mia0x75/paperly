/**
 * Shared base class names for form controls.
 *
 * Pages each had their own copy and they drifted (one gained a focus ring, another
 * didn't). One constant keeps the site in sync. Lives in lib/, not components/
 * (a class-name constant, not a .vue component).
 */
export const FIELD_BASE =
  "bg-background focus-visible:ring-ring rounded-md border px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-offset-0";

export interface SelectOption<T extends string | number = string> {
  value: T;
  label: string;
  disabled?: boolean;
}