/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export function addCalendarMonthToUnix(timestamp) {
  const value = Number(timestamp);
  if (!Number.isFinite(value) || value <= 0) return null;

  const base = new Date(value * 1000);
  if (Number.isNaN(base.getTime())) return null;

  const year = base.getFullYear();
  const month = base.getMonth();
  const day = base.getDate();
  const lastDayOfTargetMonth = new Date(year, month + 2, 0).getDate();
  const target = new Date(
    year,
    month + 1,
    Math.min(day, lastDayOfTargetMonth),
    base.getHours(),
    base.getMinutes(),
    base.getSeconds(),
    base.getMilliseconds(),
  );

  return Math.floor(target.getTime() / 1000);
}

export function getBrowserTimezone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
}
