/**
 * renderTimer -- page render time (footer right line).
 *
 * Footer isn't remounted on route change, so onMounted can only measure the first load.
 * Split in two: router.beforeEach -> markRouteStart(); Footer on route stable + next
 * frame -> elapsedSinceRouteStart(). Uses performance.now() (monotonic, clock-proof).
 */
let routeStart = 0;

export function markRouteStart(): void {
  routeStart = performance.now();
}

/** ms since markRouteStart; 0 if never marked. */
export function elapsedSinceRouteStart(): number {
  if (routeStart === 0) return 0;
  return Math.round(performance.now() - routeStart);
}
