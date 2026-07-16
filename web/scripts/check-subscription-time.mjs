import assert from 'node:assert/strict';
import {
  addCalendarMonthToUnix,
  getBrowserTimezone,
} from '../src/components/table/users/subscriptionTime.js';

function unix(value) {
  return Math.floor(new Date(value).getTime() / 1000);
}

function iso(timestamp) {
  return new Date(timestamp * 1000).toISOString();
}

assert.equal(
  iso(addCalendarMonthToUnix(unix('2026-07-16T10:20:30+08:00'))),
  '2026-08-16T02:20:30.000Z',
);
assert.equal(
  iso(addCalendarMonthToUnix(unix('2026-01-31T10:20:30+08:00'))),
  '2026-02-28T02:20:30.000Z',
);
assert.equal(
  iso(addCalendarMonthToUnix(unix('2028-01-31T10:20:30+08:00'))),
  '2028-02-29T02:20:30.000Z',
);
assert.equal(
  iso(addCalendarMonthToUnix(unix('2026-12-31T10:20:30+08:00'))),
  '2027-01-31T02:20:30.000Z',
);
assert.equal(getBrowserTimezone(), 'Asia/Shanghai');
assert.equal(addCalendarMonthToUnix(0), null);

console.log('subscription-time checks passed');
