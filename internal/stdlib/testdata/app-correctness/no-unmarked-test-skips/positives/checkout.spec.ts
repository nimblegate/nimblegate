describe('checkout', () => {
  it.only('charges tax', () => {
    expect(tax(100)).toBe(8);
  });
});
