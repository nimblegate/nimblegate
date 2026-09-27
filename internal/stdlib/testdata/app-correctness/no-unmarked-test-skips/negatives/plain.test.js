// it.skip was removed once the fix landed
describe('cart', () => {
  it('applies coupon', () => {
    expect(total()).toBe(90);
  });
});
