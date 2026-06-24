// Positive: TODO with no owner or expiry date. Will rot in the codebase.
// TODO: clean up this error handling
function process(input) {
  try {
    return JSON.parse(input);
  } catch (e) {
    return null;
  }
}
