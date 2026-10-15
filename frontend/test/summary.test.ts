/**
 * test/summary.test.ts -- summary segment parsing + sanitizing (pure logic + DOM).
 *
 * parseSummary splits the stored append-only summary on signed markers and runs
 * the body through a whitelist sanitizer before it is rendered with v-html.
 */
import "./setup";
import { assert, assertEqual, assertIncludes, assertNotIncludes, type TestSuite } from "./assert";
import {
  parseSummary,
  canDeleteSummarySegment,
  SUMMARY_AUTHOR_KID,
} from "../src/lib/summary";

// Mirrors the backend domain.UploadedByParent (1); production code only needs
// SUMMARY_AUTHOR_KID, so the parent value lives locally in the test.
const SUMMARY_AUTHOR_PARENT = 1;

const marker = (sig: string, type: number, id: number, text: string): string =>
  `<div class="summary-sign" data-sig="${sig}" data-author-type="${type}" data-author="${id}">${text}</div>`;

const suite: TestSuite = {
  name: "summary（追加式总结解析与净化）",
  cases: [
    ["空内容返回空数组", async () => {
      assertEqual(parseSummary("").length, 0, "empty");
      assertEqual(parseSummary("   \n ").length, 0, "blank");
    }],

    ["按标记切分为多段并读取署名", async () => {
      const html =
        marker("aaa11111", SUMMARY_AUTHOR_PARENT, 1, "妈妈 · 2026-10-11 09:30") +
        "<p>第一段</p>" +
        marker("bbb22222", SUMMARY_AUTHOR_KID, 7, "小明 · 2026-10-11 10:00") +
        "<p>第二段</p>";
      const segs = parseSummary(html);
      assertEqual(segs.length, 2, "segment count");
      assertEqual(segs[0]!.sig, "aaa11111", "sig1");
      assertEqual(segs[0]!.authorType, SUMMARY_AUTHOR_PARENT, "type1");
      assertEqual(segs[0]!.authorId, 1, "id1");
      assertIncludes(segs[0]!.signText, "妈妈", "sign text");
      assertIncludes(segs[0]!.bodyHtml, "第一段", "body1");
      assertEqual(segs[1]!.authorType, SUMMARY_AUTHOR_KID, "type2");
      assertEqual(segs[1]!.authorId, 7, "id2");
      assertIncludes(segs[1]!.bodyHtml, "第二段", "body2");
    }],

    ["标记前的前缀成为无署名段（不可删除）", async () => {
      const html = "<p>旧的手写总结</p>" + marker("ccc33333", SUMMARY_AUTHOR_PARENT, 1, "妈妈");
      const segs = parseSummary(html);
      assertEqual(segs.length, 2, "count");
      assertEqual(segs[0]!.hasSign, false, "prefix has no sign");
      assertIncludes(segs[0]!.bodyHtml, "旧的手写总结", "prefix body");
      assertEqual(segs[1]!.hasSign, true, "marker has sign");
    }],

    ["script/style 子树整体丢弃", async () => {
      const html = marker("ddd44444", SUMMARY_AUTHOR_PARENT, 1, "妈妈") +
        '<p>safe</p><script>alert(1)</script><style>p{color:red}</style>';
      const segs = parseSummary(html);
      const body = segs[0]!.bodyHtml;
      assertNotIncludes(body, "alert(1)", "script content dropped");
      assertNotIncludes(body, "color:red", "style content dropped");
      assertIncludes(body, "safe", "kept body");
    }],

    ["事件属性与危险标签被剥离", async () => {
      const html = marker("eee55555", SUMMARY_AUTHOR_PARENT, 1, "妈妈") +
        '<p onclick="evil()">x</p><img src=x onerror="evil()"><svg onload="evil()"></svg>';
      const body = parseSummary(html)[0]!.bodyHtml;
      assertNotIncludes(body, "onclick", "onclick stripped");
      assertNotIncludes(body, "onerror", "onerror stripped");
      assertNotIncludes(body, "onload", "onload stripped");
      assertNotIncludes(body, "<img", "img not rendered as raw tag");
      assertNotIncludes(body, "<svg", "svg dropped");
      assertIncludes(body, "<p>x</p>", "text kept");
    }],

    ["链接：javascript 被丢弃，https 保留并加 rel", async () => {
      const bad = parseSummary(
        marker("fff66666", SUMMARY_AUTHOR_PARENT, 1, "妈妈") +
          '<a href="javascript:alert(1)">click</a>',
      )[0]!.bodyHtml;
      assertNotIncludes(bad, "javascript:", "javascript href dropped");
      assertIncludes(bad, "click", "link text kept");

      const good = parseSummary(
        marker("ggg77777", SUMMARY_AUTHOR_PARENT, 1, "妈妈") +
          '<a href="https://example.com/a">site</a>',
      )[0]!.bodyHtml;
      assertIncludes(good, 'href="https://example.com/a"', "https href kept");
      assertIncludes(good, 'rel="noopener noreferrer"', "rel added");
    }],

    ["允许标签保留：加粗/斜体/列表/标题", async () => {
      const body = parseSummary(
        marker("hhh88888", SUMMARY_AUTHOR_PARENT, 1, "妈妈") +
          "<h2>标题</h2><ul><li><strong>粗</strong></li></ul>",
      )[0]!.bodyHtml;
      assertIncludes(body, "<h2>标题</h2>", "heading");
      assertIncludes(body, "<ul>", "list");
      assertIncludes(body, "<strong>粗</strong>", "bold");
    }],

    ["删除权限：家长任意、孩子仅本人", async () => {
      const parentSeg = parseSummary(
        marker("iii11111", SUMMARY_AUTHOR_PARENT, 1, "妈妈") + "<p>a</p>",
      )[0]!;
      const kidOwn = parseSummary(
        marker("jjj22222", SUMMARY_AUTHOR_KID, 7, "小明") + "<p>b</p>",
      )[0]!;
      const kidOther = parseSummary(
        marker("kkk33333", SUMMARY_AUTHOR_KID, 8, "小红") + "<p>c</p>",
      )[0]!;

      // Parent may delete any signed segment.
      assert(canDeleteSummarySegment(parentSeg, { isKid: false, selfKidId: null }), "parent deletes parent");
      assert(canDeleteSummarySegment(kidOwn, { isKid: false, selfKidId: null }), "parent deletes kid");

      // Kid may only delete their own.
      assert(canDeleteSummarySegment(kidOwn, { isKid: true, selfKidId: 7 }), "kid deletes own");
      assert(!canDeleteSummarySegment(parentSeg, { isKid: true, selfKidId: 7 }), "kid cannot delete parent");
      assert(!canDeleteSummarySegment(kidOther, { isKid: true, selfKidId: 7 }), "kid cannot delete other kid");
    }],

    ["无署名前缀不可删除", async () => {
      const prefix = parseSummary("<p>legacy</p>")[0]!;
      assert(!canDeleteSummarySegment(prefix, { isKid: false, selfKidId: null }), "prefix not deletable");
    }],
  ],
};

export default suite;
