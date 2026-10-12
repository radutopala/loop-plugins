// Hash routing: the frame can't navigate, but it can change its hash.

export type Route = "count" | "about";

export const ROUTES: readonly Route[] = ["count", "about"];

/** The route a location hash names, "count" for any other. */
export function parseRoute(hash: string): Route {
  const name = hash.replace(/^#\/?/, "");
  return (ROUTES as readonly string[]).includes(name) ? (name as Route) : "count";
}

/** The hash that shows route. */
export function routeHash(route: Route): string {
  return `#/${route}`;
}
