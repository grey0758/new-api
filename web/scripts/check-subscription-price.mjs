import assert from 'node:assert/strict';
import {
  getSubscriptionPrice,
  formatSubscriptionPrice,
} from '../src/helpers/subscriptionPrice.mjs';

for (const price of [160, 300, 600, 1200]) {
  const plan = { currency: 'CNY', price_amount: price };
  for (const display of [
    { symbol: '$', rate: 1 },
    { symbol: '¥', rate: 7 },
    { symbol: '¤', rate: 3 },
  ]) {
    assert.equal(formatSubscriptionPrice(plan, display), `¥${price}.00`);
    assert.equal(getSubscriptionPrice(plan, display).displayPrice, `${price}`);
  }
}
assert.equal(
  formatSubscriptionPrice(
    { currency: 'USD', price_amount: 4 },
    { symbol: '¥', rate: 7 },
  ),
  '¥28.00',
);
assert.equal(formatSubscriptionPrice({ price_amount: 4 }), '$4.00');
assert.equal(
  getSubscriptionPrice({ currency: 'CNY', price_amount: 160.5 }).displayPrice,
  '160.50',
);
console.log('subscription currency display checks passed');
