import assert from 'node:assert/strict';

import {
  REGISTRATION_CHALLENGE_VERSION,
  buildRegistrationChallengeMaterial,
  sha256Hex,
  solveRegistrationChallenge,
} from '../src/helpers/registrationChallenge.js';

const fixedChallenge = {
  version: REGISTRATION_CHALLENGE_VERSION,
  challengeId: 'AAAAAAAAAAAAAAAAAAAAAA',
  seed: 'BBBBBBBBBBBBBBBBBBBBBB',
  targetHash:
    '2bd806c97f0e00af1a1fc3328fa763a9269723c8db8fac4f93af71db186d6e90',
  difficulty: 3,
  expiresAt: Math.floor(Date.now() / 1000) + 60,
  expiresIn: 60,
};

const material = buildRegistrationChallengeMaterial(fixedChallenge, 42);
assert.equal(
  await sha256Hex(material),
  'e543aa2b08071795fec81a2f085c3b0f10884d91a94113b0e0cb4cd2d3952567',
);

const token = await solveRegistrationChallenge(fixedChallenge, {
  maxSolveMs: 10000,
  yieldEvery: 0,
});
const [version, challengeId, nonce, digest] = token.split('.');
assert.equal(version, REGISTRATION_CHALLENGE_VERSION);
assert.equal(challengeId, fixedChallenge.challengeId);
assert.match(nonce, /^\d+$/);
assert.match(digest, /^000[0-9a-f]{61}$/);
assert.equal(
  digest,
  await sha256Hex(
    buildRegistrationChallengeMaterial(fixedChallenge, Number(nonce)),
  ),
);

console.log('registration challenge frontend checks passed');
