/**
 * test/render-timer.test.ts -- footer render timing (src/lib/renderTimer.ts).
 *
 * If the start point (routeStart) isn't marked or is read too early, the elapsed
 * time reads as 0 forever. This pins the computation layer's two invariants; the
 * router wiring is guaranteed by structure (see src/router/index.ts).
 */
import { assert, type TestSuite } from "./assert";
import { elapsedSinceRouteStart, markRouteStart } from "../src/lib/renderTimer";

const suite: TestSuite = {
  name: "renderTimer（页脚渲染计时）",
  cases: [
    ["mark 后能测到 >0 ms 且单调不减", async () => {
      markRouteStart();
      const t0 = elapsedSinceRouteStart();
      // Wait a real beat with setTimeout: if mark didn't take effect, this stays 0
      // and the case fails.
      await new Promise((r) => setTimeout(r, 5));
      const t1 = elapsedSinceRouteStart();
      assert(t1 >= 1, `mark 后应测到 >0 ms，实际 t1=${t1}（计时器没生效）`);
      assert(t1 >= t0, `耗时应随时间只增不减：t0=${t0}, t1=${t1}`);
    }],
  ],
};

export default suite;