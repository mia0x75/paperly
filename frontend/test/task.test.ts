/**
 * test/task.test.ts -- task duration formatting for the async/background queue log viewer.
 *
 * Pure logic, no DOM. The duration is derived from two second-precision timestamps
 * ("YYYY-MM-DD HH:mm:ss"), so the contract pinned here is:
 *   - fractional seconds are impossible (backend formats to whole seconds);
 *   - the three ranges (< 1m / < 1h / >= 1h) format distinctly;
 *   - missing / malformed / negative ranges return "" (caller omits the parenthetical).
 */
import { assertEqual, type TestSuite } from "./assert";
import { taskDurationText } from "../src/lib/task";

const suite: TestSuite = {
  name: "任务时长格式化（后台队列日志）",
  cases: [
    ["秒级：整秒显示不带小数", async () => {
      assertEqual(
        taskDurationText("2026-10-12 10:00:01", "2026-10-12 10:00:05"),
        "4s",
        "4 秒",
      );
      assertEqual(taskDurationText("2026-10-12 10:00:00", "2026-10-12 10:00:00"), "0s", "0 秒");
    }],

    ["分钟级：Xm 秒补零", async () => {
      assertEqual(taskDurationText("2026-10-12 10:00:00", "2026-10-12 10:01:00"), "1m00s", "恰好 60 秒");
      assertEqual(taskDurationText("2026-10-12 10:00:00", "2026-10-12 10:01:23"), "1m23s", "83 秒");
    }],

    ["小时级：Xh 分补零", async () => {
      assertEqual(taskDurationText("2026-10-12 10:00:00", "2026-10-12 11:02:05"), "1h02m", "1 小时 2 分");
    }],

    ["缺失时间戳返回空串", async () => {
      assertEqual(taskDurationText("", "2026-10-12 10:00:05"), "", "无开始");
      assertEqual(taskDurationText("2026-10-12 10:00:01", ""), "", "无完成");
    }],

    ["格式非法返回空串", async () => {
      assertEqual(taskDurationText("not-a-time", "2026-10-12 10:00:05"), "", "开始非法");
      assertEqual(taskDurationText("2026-10-12 10:00:01", "2026/10/12 10:00:05"), "", "完成非法");
    }],

    ["负区间返回空串（完成早于开始）", async () => {
      assertEqual(
        taskDurationText("2026-10-12 10:00:05", "2026-10-12 10:00:01"),
        "",
        "完成早于开始",
      );
    }],
  ],
};

export default suite;
