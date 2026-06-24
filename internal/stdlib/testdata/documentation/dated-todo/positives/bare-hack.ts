// HACK: temporary workaround until the upstream fix lands
export function workaround(value: string): string {
  return value.replace(/\s+/g, "");
}
