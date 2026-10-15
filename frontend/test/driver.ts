/**
 * test/driver.ts -- shared suite driver.
 *
 * Runs TestSuites sequentially in ONE process: cases share the happy-dom document
 * / localStorage / fetch stub installed by test/setup, so suites must NOT run
 * concurrently in-process. Parallelism happens one level up, by running separate
 * lane entry points (run-unit / run-component / run-page) as separate OS
 * processes, each with its own globals.
 */
import type { TestSuite } from "./assert";

export async function runSuites(suites: TestSuite[]): Promise<void> {
  let passed = 0;
  const failures: string[] = [];

  for (const suite of suites) {
    console.log(`\n${suite.name}`);
    for (const [name, run] of suite.cases) {
      try {
        await run();
        passed++;
        console.log(`  ✓ ${name}`);
      } catch (err) {
        failures.push(`${suite.name} › ${name}: ${err instanceof Error ? err.message : String(err)}`);
        console.log(`  ✗ ${name}`);
      }
    }
  }

  console.log(`\n通过 ${passed}，失败 ${failures.length}`);
  for (const f of failures) console.log(`  - ${f}`);
  process.exit(failures.length === 0 ? 0 : 1);
}
