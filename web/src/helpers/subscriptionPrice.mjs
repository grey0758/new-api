// Subscription prices have their own settlement currency; quota display does not
// change a CNY sale price. Legacy USD plans retain the chosen display conversion.
export function getSubscriptionPrice(plan, displayCurrency = {}) {
  const cny = plan?.currency?.toUpperCase() === 'CNY';
  const symbol = cny ? '¥' : displayCurrency.symbol || '$';
  const rate = cny ? 1 : (displayCurrency.rate ?? 1);
  const amount = Number(plan?.price_amount || 0) * rate;
  return {
    symbol,
    amount,
    displayPrice: amount.toFixed(Number.isInteger(amount) ? 0 : 2),
  };
}

export function formatSubscriptionPrice(plan, displayCurrency, digits = 2) {
  const { symbol, amount } = getSubscriptionPrice(plan, displayCurrency);
  return symbol + amount.toFixed(digits);
}
