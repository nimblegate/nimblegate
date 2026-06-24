// Comment showing an accountable marker (owner + ISO date + issue).
// TODO(@alice 2026-08-15): rework error handling once issue #234 lands
function process(input) {
  try {
    return JSON.parse(input);
  } catch (e) {
    return null;
  }
}
