import { defineStore } from "pinia";
import { ref } from "vue";
import { kidApi } from "../api/kid";
import { KID_STATUS, type KidSummary } from "../api/types";

/** Student list + "currently selected student" (selectedId persists to localStorage). */
const SELECTED_KEY = "paperly.selected_kid_id";

export const useKidStore = defineStore("kids", () => {
  const list = ref<KidSummary[]>([]);
  const loading = ref(false);
  const error = ref("");

  const selectedId = ref<number | null>(readSelected());

  /** Safe storage access: mirrors lib/*Session — missing localStorage or a
      SecurityError (e.g. Safari private mode) degrades to memory-only state. */
  function storage(): Storage | null {
    try {
      return typeof localStorage === "undefined" ? null : localStorage;
    } catch {
      return null;
    }
  }

  function readSelected(): number | null {
    const raw = storage()?.getItem(SELECTED_KEY);
    if (!raw) return null;
    const n = Number(raw);
    return Number.isFinite(n) && n > 0 ? n : null;
  }

  function select(id: number | null): void {
    selectedId.value = id;
    try {
      if (id === null) {
        storage()?.removeItem(SELECTED_KEY);
      } else {
        storage()?.setItem(SELECTED_KEY, String(id));
      }
    } catch {
      // Persistence is best-effort; in-memory selection still applies.
    }
  }

  async function fetchList(): Promise<void> {
    loading.value = true;
    error.value = "";
    try {
      // Fall back to []: /api/kids is contractually an array, but if null/missing fields leak
      // through, the next line's list.value.some would throw and blank the whole page — an empty
      // array is semantically equivalent and cannot explode.
      list.value = (await kidApi.list()) ?? [];
      // Clear the selection when the currently selected student no longer exists, to avoid a dangling context.
      if (selectedId.value !== null && !list.value.some((s) => s.id === selectedId.value)) {
        select(null);
      }
      // Default to the "first active student" rather than list[0]:
      // the list also contains graduated students; setting a graduate as the default current
      // student would show a parent an empty panel of a kid who has already left.
      if (selectedId.value === null) {
        const firstActive = list.value.find((s) => s.status === KID_STATUS.ACTIVE);
        if (firstActive !== undefined) select(firstActive.id);
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
  }

  return { list, loading, error, selectedId, select, fetchList };
});
