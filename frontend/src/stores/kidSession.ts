import { defineStore } from "pinia";
import { ref } from "vue";
import { readKidId, readKidName, readKidGrade, readKidTrack, saveKidId, saveKidName, saveKidGrade, saveKidTrack } from "../lib/kidSession";

/**
 * Reactive kid session profile (name + kid id + grade), persisted to localStorage.
 *
 * The home page learns these from /api/me, but the top bar and list/entry pages are different
 * components and need the values right away (top bar title, "only my exams" kid_id filter,
 * default grade). Storing them here keeps those in sync after a refresh.
 *
 * Pinia (not a module-level ref) so each mount gets a fresh instance and tests don't share
 * state, matching stores/kid.ts. The credential (paperly.kid_token) is not here — guards and
 * the request layer read it via the plain lib, synchronously.
 */
export const useKidSessionStore = defineStore("kidSession", () => {
  const name = ref(readKidName());
  const kidId = ref<number | null>(readKidId());
  const grade = ref(readKidGrade());
  const track = ref(readKidTrack());

  function setKidName(next: string): void {
    saveKidName(next);
    name.value = next;
  }

  function setKidId(next: number | null): void {
    saveKidId(next);
    kidId.value = next;
  }

  function setGrade(next: number): void {
    saveKidGrade(next);
    grade.value = next;
  }

  function setTrack(next: number): void {
    saveKidTrack(next);
    track.value = next;
  }

  return { name, kidId, grade, track, setKidName, setKidId, setGrade, setTrack };
});
