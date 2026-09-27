export function price(items, taxRate = 0.08) {
  const sub = items.reduce((a, b) => a + b.price, 0);
  return sub * (1 + taxRate);
}
