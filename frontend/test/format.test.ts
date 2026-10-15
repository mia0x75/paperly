/** test/format.test.ts -- lib/format dashboard figure formatting. */
import { assertEqual, type TestSuite } from "./assert";
import { avgPagesPerExam } from "../src/lib/format";

const suite: TestSuite = {
  name: "format（看板数字格式化）",
  cases: [
    ["平均页数保留一位小数", () => {
      assertEqual(avgPagesPerExam(10, 3), "平均 3.3 页/份", "平均页格式");
    }],
    ["总量为 0 返回空串（调用方自行展示“尚无归档”）", () => {
      assertEqual(avgPagesPerExam(0, 0), "", "0/0 无比值");
      assertEqual(avgPagesPerExam(5, 0), "", "0 总量无此值");
    }],
  ],
};

export default suite;