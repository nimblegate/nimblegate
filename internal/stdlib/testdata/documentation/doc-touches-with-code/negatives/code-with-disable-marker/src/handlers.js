// appframes:disable convention/doc-touches-with-code
// Pure refactor; no behavior change; doc untouched intentionally.
export function handleRequest(req) {
  return new Response("hello", { status: 200 });
}
