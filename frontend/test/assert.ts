/**
 * test/assert.ts -- minimal assertions.
 *
 * Why not vitest: see README.md.
 */

/** A test case: name + actual runner. */
export type TestCase = [name: string, run: () => void | Promise<void>];

/** A test file: filename + case list. */
export interface TestSuite {
  name: string;
  cases: TestCase[];
}

export class AssertionError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AssertionError";
  }
}

export function assert(condition: unknown, message: string): asserts condition {
  if (!condition) throw new AssertionError(message);
}

/** Strict equality; the error message includes the actual value. */
export function assertEqual<T>(actual: T, expected: T, message: string): void {
  if (actual !== expected) {
    throw new AssertionError(`${message}(expected ${String(expected)}, actual ${String(actual)})`);
  }
}

export function assertIncludes(text: string, fragment: string, message: string): void {
  if (!text.includes(fragment)) {
    throw new AssertionError(`${message}("${fragment}" not found)`);
  }
}

export function assertNotIncludes(text: string, fragment: string, message: string): void {
  if (text.includes(fragment)) {
    throw new AssertionError(`${message}("${fragment}" should not appear)`);
  }
}
