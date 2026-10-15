/**
 * test/exam-title.test.ts -- genTitle rules (pure logic, no DOM).
 *
 * Rules: weekly/monthly/mock/special/other carry the month; midterm/final do not.
 * The term follows the Chinese school year: Sep 1 - Jan 31 next year is first
 * term, Feb 1 - Jun is second term.
 */
import { assertEqual, type TestSuite } from "./assert";
import { genTitle, termOf, EXAM_TYPE } from "../src/lib/examTitle";

const suite: TestSuite = {
  name: "genTitle（试卷标题规则）",
  cases: [
    ["月考带月份", async () => {
      assertEqual(
        genTitle("2026-09-25", 9, "数学", EXAM_TYPE.monthly),
        "2026-初三-上学期-数学-09月-月考",
        "月考",
      );
    }],

    ["周测带月份", async () => {
      assertEqual(
        genTitle("2026-10-08", 9, "语文", EXAM_TYPE.weekly),
        "2026-初三-上学期-语文-10月-周测",
        "genTitle",
      );
    }],

    ["期中不带月份", async () => {
      assertEqual(
        genTitle("2026-11-10", 9, "数学", EXAM_TYPE.midterm),
        "2026-初三-上学期-数学-期中",
        "genTitle",
      );
    }],

    ["期末不带月份（春季学期）", async () => {
      // April is second term and there is no double dash, so the correct output is:
      assertEqual(
        genTitle("2027-04-15", 9, "数学", EXAM_TYPE.final),
        "2027-初三-下学期-数学-期末",
        "genTitle",
      );
    }],

    ["模考带月份", async () => {
      assertEqual(
        genTitle("2026-09-25", 9, "物理", EXAM_TYPE.mock),
        "2026-初三-上学期-物理-09月-模考",
        "genTitle",
      );
    }],

    ["专项带月份", async () => {
      assertEqual(
        genTitle("2026-09-25", 9, "数学", EXAM_TYPE.special),
        "2026-初三-上学期-数学-09月-专项",
        "genTitle",
      );
    }],

    ["其他带月份", async () => {
      assertEqual(
        genTitle("2026-03-02", 6, "科学", EXAM_TYPE.other),
        "2026-六年级-下学期-科学-03月-其他",
        "genTitle",
      );
    }],

    ["月份补零", async () => {
      assertEqual(
        genTitle("2026-02-05", 9, "数学", EXAM_TYPE.monthly),
        "2026-初三-下学期-数学-02月-月考",
        "genTitle",
      );
    }],

    ["1 月属于上学期", async () => {
      assertEqual(termOf(new Date(2027, 0, 15)), "上学期", "1 月");
    }],

    ["9 月属于上学期", async () => {
      assertEqual(termOf(new Date(2026, 8, 1)), "上学期", "9 月");
    }],

    ["5 月属于下学期", async () => {
      assertEqual(termOf(new Date(2026, 4, 1)), "下学期", "5 月");
    }],

    ["缺科目返回空串，不生成残标题", async () => {
      assertEqual(genTitle("2026-09-25", 9, "", EXAM_TYPE.monthly), "", "缺科目");
    }],

    ["缺日期返回空串", async () => {
      assertEqual(genTitle("", 9, "数学", EXAM_TYPE.monthly), "", "缺日期");
    }],

    ["非法年级返回空串", async () => {
      assertEqual(genTitle("2026-09-25", 0, "数学", EXAM_TYPE.monthly), "", "非法年级");
    }],

    ["未知考试类型返回空串", async () => {
      assertEqual(genTitle("2026-09-25", 9, "数学", 123), "", "未知类型");
    }],
  ],
};

export default suite;
