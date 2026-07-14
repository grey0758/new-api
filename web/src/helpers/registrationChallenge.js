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

export const REGISTRATION_CHALLENGE_VERSION = 'newapi-register-v1';

const DEFAULT_MAX_SOLVE_MS = 10000;
const DEFAULT_YIELD_EVERY = 128;
const encoder = new TextEncoder();

export class RegistrationChallengeError extends Error {
  constructor(message) {
    super(message);
    this.name = 'RegistrationChallengeError';
  }
}

export function buildRegistrationChallengeMaterial(challenge, nonce) {
  return [
    REGISTRATION_CHALLENGE_VERSION,
    challenge.challengeId,
    challenge.seed,
    challenge.targetHash,
    String(nonce),
  ].join(':');
}

export async function sha256Hex(value) {
  if (!globalThis.crypto?.subtle) {
    throw new RegistrationChallengeError(
      '当前浏览器不支持注册安全验证，请升级浏览器后重试',
    );
  }
  const digest = await globalThis.crypto.subtle.digest(
    'SHA-256',
    encoder.encode(value),
  );
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('');
}

function validateRegistrationChallenge(challenge) {
  const isBase64Url128 = (value) =>
    typeof value === 'string' && /^[A-Za-z0-9_-]{22}$/.test(value);
  if (
    !challenge ||
    challenge.version !== REGISTRATION_CHALLENGE_VERSION ||
    !isBase64Url128(challenge.challengeId) ||
    !isBase64Url128(challenge.seed) ||
    !/^[0-9a-f]{64}$/.test(challenge.targetHash || '') ||
    !Number.isInteger(challenge.difficulty) ||
    challenge.difficulty < 1 ||
    challenge.difficulty > 8 ||
    !Number.isFinite(challenge.expiresAt) ||
    !Number.isFinite(challenge.expiresIn) ||
    challenge.expiresIn <= 0 ||
    challenge.expiresIn > 600
  ) {
    throw new RegistrationChallengeError('注册安全验证参数无效，请刷新后重试');
  }
}

export async function solveRegistrationChallenge(challenge, options = {}) {
  validateRegistrationChallenge(challenge);

  const maxSolveMs = options.maxSolveMs ?? DEFAULT_MAX_SOLVE_MS;
  const yieldEvery = options.yieldEvery ?? DEFAULT_YIELD_EVERY;
  const startedAt = Date.now();
  const challengeLifetimeMs = challenge.expiresIn * 1000;
  const deadline =
    startedAt + Math.min(maxSolveMs, Math.max(challengeLifetimeMs - 250, 0));
  const requiredPrefix = '0'.repeat(challenge.difficulty);

  if (deadline <= startedAt) {
    throw new RegistrationChallengeError('注册安全验证已过期，请重新提交');
  }

  for (let nonce = 0; Number.isSafeInteger(nonce); nonce += 1) {
    if (Date.now() >= deadline) {
      break;
    }
    const digest = await sha256Hex(
      buildRegistrationChallengeMaterial(challenge, nonce),
    );
    if (digest.startsWith(requiredPrefix)) {
      return `${REGISTRATION_CHALLENGE_VERSION}.${challenge.challengeId}.${nonce}.${digest}`;
    }
    if (yieldEvery > 0 && nonce > 0 && nonce % yieldEvery === 0) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
  }

  throw new RegistrationChallengeError(
    '注册安全验证计算超时，请关闭省电模式或更换浏览器后重试',
  );
}
