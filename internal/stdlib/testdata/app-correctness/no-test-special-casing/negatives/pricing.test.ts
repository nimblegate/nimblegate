process.env.NODE_ENV = 'test';
it('prices', () => expect(price([])).toBe(0));
