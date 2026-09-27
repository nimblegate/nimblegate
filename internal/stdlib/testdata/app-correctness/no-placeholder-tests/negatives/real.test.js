describe('cart', () => {
  it('applies coupon', () => {
    expect(total(100, 'SAVE10')).toBe(90);
  });
  it.todo('handles expired coupons');
});
