/**
 * test/message-kind.test.ts -- notification kind labels (lib/message.ts kindLabel).
 *
 * MESSAGE_KIND must stay in lockstep with backend domain/message.go (kinds
 * 10/11 = grade-10/grade-7 reminders). Unknown future kinds degrade to "通知"
 * instead of blanking the bell.
 */
import "./setup";
import { assertEqual, type TestSuite } from "./assert";
import { MESSAGE_KIND } from "../src/api/types";
import { kindLabel } from "../src/lib/message";

const suite: TestSuite = {
  name: "通知类型标签（kind 10/11 回归）",
  cases: [
    ["kind 常量与后端对齐", async () => {
      assertEqual(MESSAGE_KIND.GRADE10_REMINDER, 10, "grade-10");
      assertEqual(MESSAGE_KIND.GRADE7_REMINDER, 11, "grade-7");
      assertEqual(MESSAGE_KIND.GRADE9_REMINDER, 9, "grade-9");
    }],

    ["10/11 有专属标签而非兜底", async () => {
      assertEqual(kindLabel(10), "高一分科提醒", "kind 10");
      assertEqual(kindLabel(11), "初一科目提醒", "kind 11");
      assertEqual(kindLabel(9), "初三科目提醒", "kind 9");
    }],

    ["未知 kind 兜底为通用标签", async () => {
      assertEqual(kindLabel(42), "通知", "unknown kind");
    }],
  ],
};

export default suite;
