/**
 * test/subject-visible.test.ts -- subject visibility (pure logic, no DOM).
 *
 * Locks the mask = 5-bit contract and the rule that a kid's arts/science track
 * only filters subjects from senior high (grade >= 10); below that the stored
 * track is ignored (a track set early must not hide subjects).
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import {
  SUBJECT_STAGE,
  SUBJECT_TRACK,
  TRACK_MIN_GRADE,
  stageBitForGrade,
  trackBitFor,
  subjectVisibleForGrade,
  subjectVisibleForTrack,
} from "../src/lib/subject";

// subjectVisible composes the two production checks; assert the combined verdict too.
const subjectVisible = (s: { mask?: number }, g: number | null, t: number | null): boolean =>
  subjectVisibleForGrade(s, g) && subjectVisibleForTrack(s, t, g);

// stage bits (low 3) + track bits (high 2).
const ALL_STAGES = SUBJECT_STAGE.PRIMARY | SUBJECT_STAGE.JUNIOR | SUBJECT_STAGE.SENIOR;
const core = { mask: ALL_STAGES | SUBJECT_TRACK.ARTS | SUBJECT_TRACK.SCIENCE }; // mask=31
// Track-specific subjects span junior+senior so the stage filter passes at grade 8
// and the assertion isolates the track dimension.
const arts = { mask: SUBJECT_STAGE.JUNIOR | SUBJECT_STAGE.SENIOR | SUBJECT_TRACK.ARTS };
const science = { mask: SUBJECT_STAGE.JUNIOR | SUBJECT_STAGE.SENIOR | SUBJECT_TRACK.SCIENCE };
// A senior-only subject exercises the stage dimension independently of track.
const seniorOnly = { mask: SUBJECT_STAGE.SENIOR | SUBJECT_TRACK.ARTS };

const suite: TestSuite = {
  name: "subjectVisible（科目可见性 · 学段+文理）",
  cases: [
    ["stageBitForGrade 分段", async () => {
      assertEqual(stageBitForGrade(3), SUBJECT_STAGE.PRIMARY, "小学");
      assertEqual(stageBitForGrade(8), SUBJECT_STAGE.JUNIOR, "初中");
      assertEqual(stageBitForGrade(11), SUBJECT_STAGE.SENIOR, "高中");
      assertEqual(stageBitForGrade(0), 0, "越界为 0");
    }],

    ["trackBitFor 未分科/越界返回 0", async () => {
      assertEqual(trackBitFor(0), 0, "未分科");
      assertEqual(trackBitFor(3), 0, "越界");
      assertEqual(trackBitFor(1), SUBJECT_TRACK.ARTS, "文科位");
      assertEqual(trackBitFor(2), SUBJECT_TRACK.SCIENCE, "理科位");
    }],

    ["trackBitFor 低于高一强制为 0", async () => {
      // 无论存了什么 track，高一以下一律不按文理过滤。
      assertEqual(trackBitFor(1, 8), 0, "初三+文科→0");
      assertEqual(trackBitFor(2, 9), 0, "初三+理科→0");
      assertEqual(trackBitFor(1, TRACK_MIN_GRADE), SUBJECT_TRACK.ARTS, "高一+文科→文科位");
    }],

    ["初中：track 不生效，全部科目可见", async () => {
      // 初二即使已设文科，理科/文科专属科目仍应可见（分科只从高中起）。
      assert(subjectVisible(science, 8, 1), "初中理科科目对文科生仍可见");
      assert(subjectVisible(arts, 8, 2), "初中文科科目对理科生仍可见");
      assert(subjectVisible(core, 8, 0), "核心科目恒可见");
    }],

    ["高中：track 生效，按文理过滤", async () => {
      assert(subjectVisible(arts, 10, 1), "高一文科生看到文科科目");
      assert(!subjectVisible(science, 10, 1), "高一文科生看不到纯理科科目");
      assert(subjectVisible(science, 10, 2), "高一理科生看到理科科目");
      assert(!subjectVisible(arts, 10, 2), "高一理科生看不到纯文科科目");
      assert(subjectVisible(core, 10, 1), "核心科目对任一分科恒可见");
      // 高中未分科（track=0）→ 不按文理过滤，两类专属科目都可见。
      assert(subjectVisible(arts, 10, 0) && subjectVisible(science, 10, 0), "高中未分科全部可见");
    }],

    ["学段维度独立于 track", async () => {
      // mask 只含高中的科目对初中生（stage 不匹配）不可见，与 track 无关。
      assert(!subjectVisibleForGrade(seniorOnly, 8), "高中科目对初中不可见");
      assert(subjectVisibleForGrade(seniorOnly, 11), "高中科目对高中可见");
    }],

    ["mask 全关（0）→ 隐藏；未定义 mask → 可见", async () => {
      assert(!subjectVisibleForGrade({ mask: 0 }, 8), "全关隐藏");
      assert(subjectVisibleForGrade({}, 8), "无 mask 字段默认可见");
      assert(!subjectVisibleForTrack({ mask: 0 }, 1, 10), "track 维度全关仍隐藏");
    }],
  ],
};

export default suite;
