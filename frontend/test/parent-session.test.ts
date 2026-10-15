/**
 * test/parent-session.test.ts -- parent login-state persistence (lib/parentSession).
 *
 * Covers read/write rules plus the redirect-target construction, pinning the
 * localStorage contract:
 *   key = "paperly.parent_token" (naming consistent with kidSession);
 *   a missing value reads back as an empty string, not null;
 *   writing an empty string, or under a no-storage environment, fails silently without throwing.
 */
import { assertEqual, type TestSuite } from "./assert";
import { resetDOM, testWindow } from "./setup";
import {
  clearParentToken,
  loginRedirectUrl,
  PARENT_TOKEN_KEY,
  readParentToken,
  writeParentToken,
} from "../src/lib/parentSession";

const suite: TestSuite = {
  name: "parentSession（家长 JWT 存取）",
  cases: [
    ["未写入时读到空串而非 null", () => {
      resetDOM();
      assertEqual(readParentToken(), "", "空会话应返回空串");
    }],

    ["写入后可读回，写空串不落盘", () => {
      resetDOM();
      writeParentToken("jwt-abc");
      assertEqual(testWindow.localStorage.getItem(PARENT_TOKEN_KEY), "jwt-abc", "写入 key");
      assertEqual(readParentToken(), "jwt-abc", "读回同一串");
      writeParentToken("");
      assertEqual(testWindow.localStorage.getItem(PARENT_TOKEN_KEY), "jwt-abc", "空串不覆盖已有 token");
      assertEqual(readParentToken(), "jwt-abc", "读回仍是原 token");
    }],

    ["clear 后恢复空串", () => {
      resetDOM();
      writeParentToken("jwt-abc");
      clearParentToken();
      assertEqual(testWindow.localStorage.getItem(PARENT_TOKEN_KEY), null, "删除 key");
      assertEqual(readParentToken(), "", "读回空串");
    }],

    ["回跳地址：子页带 redirect 参数", () => {
      assertEqual(
        loginRedirectUrl("/storage"),
        "/login?redirect=%2Fstorage",
        "非登录页应带回跳参数",
      );
    }],

    ["回跳地址：查询串原样编码携带", () => {
      assertEqual(
        loginRedirectUrl("/exams?kid_id=7"),
        "/login?redirect=%2Fexams%3Fkid_id%3D7",
        "查询参数应被编码",
      );
    }],

    ["回跳地址：已在登录页返回 null（防自跳环）", () => {
      assertEqual(loginRedirectUrl("/login?redirect=%2F"), null, "登录页内应跳过");
    }],
  ],
};

export default suite;