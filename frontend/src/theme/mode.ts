import { readonly, ref } from "vue";

/**
 * Theme mode (light / dark / system).
 * Dark-first design; defaults to dark.
 *
 * Mechanism: dark default (first visit follows system); persisted (paperly-theme);
 * switch flips <html class="dark">.
 */
export type ThemeMode = "light" | "dark" | "system";

const STORAGE_KEY = "paperly-theme";

const mode = ref<ThemeMode>(readStored());
const resolved = ref<"light" | "dark">("dark");

/** System preference query (treats SSR / missing matchMedia as dark). */
function systemPrefersDark(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return true;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/** Safe storage read: mirrors lib/*Session — a SecurityError (e.g. Safari
    private mode) falls back to the system preference instead of throwing. */
function readStoredItem(): string | null {
  try {
    if (typeof localStorage === "undefined") return null;
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

function readStored(): ThemeMode {
  const raw = readStoredItem();
  if (raw === "light" || raw === "dark" || raw === "system") return raw;
  // First visit: follow the system.
  return systemPrefersDark() ? "dark" : "light";
}

/** Effective light/dark (resolves system preference). */
function resolve(m: ThemeMode): "light" | "dark" {
  if (m === "system") return systemPrefersDark() ? "dark" : "light";
  return m;
}

function applyMode(): void {
  const effective = resolve(mode.value);
  resolved.value = effective;
  if (typeof document !== "undefined") {
    document.documentElement.classList.toggle("dark", effective === "dark");
  }
}

let mediaQuery: MediaQueryList | null = null;

function onSystemChange(): void {
  if (mode.value === "system") applyMode();
}

/** Init: apply current mode + listen for system changes (system mode only). */
export function initThemeMode(): void {
  applyMode();
  if (typeof window === "undefined" || !window.matchMedia) return;
  // Guard against double registration (e.g. HMR re-invoking init): the same
  // query + handler pair is registered at most once, the stale one removed.
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  if (mediaQuery !== null && mediaQuery !== mq) {
    mediaQuery.removeEventListener("change", onSystemChange);
  }
  if (mediaQuery !== mq) {
    mediaQuery = mq;
    mq.addEventListener("change", onSystemChange);
  }
}

/** setThemeMode switches the mode and persists it (persistence is best-effort). */
export function setThemeMode(next: ThemeMode): void {
  mode.value = next;
  try {
    if (typeof localStorage !== "undefined") {
      localStorage.setItem(STORAGE_KEY, next);
    }
  } catch {
    // Private-mode write failure must not break the theme switch itself.
  }
  applyMode();
}

/** Toggle light/dark (system counts as the opposite of resolved). */
export function toggleThemeMode(): void {
  setThemeMode(resolved.value === "dark" ? "light" : "dark");
}

export function useThemeMode() {
  return {
    mode: readonly(mode),
    resolved: readonly(resolved),
    setMode: setThemeMode,
    toggle: toggleThemeMode,
  };
}
