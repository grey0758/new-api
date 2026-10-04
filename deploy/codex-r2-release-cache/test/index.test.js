import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import test from 'node:test';
import worker, { syncAndRecord } from '../src/index.js';

const targets = [
  'x86_64-unknown-linux-musl', 'aarch64-unknown-linux-musl',
  'x86_64-apple-darwin', 'aarch64-apple-darwin',
  'x86_64-pc-windows-msvc', 'aarch64-pc-windows-msvc',
];
const names = [...targets.map((target) => `codex-package-${target}.tar.gz`), 'codex-package_SHA256SUMS'];

function release(version) {
  return {
    tag_name: `rust-v${version}`,
    assets: names.map((name) => ({
      name,
      digest: `sha256:${createHash('sha256').update(`${version}:${name}`).digest('hex')}`,
    })),
  };
}

function bucket() {
  const objects = new Map();
  const object = (entry) => entry && {
    body: entry.bytes,
    text: async () => new TextDecoder().decode(entry.bytes),
    checksums: { sha256: entry.digest },
    size: entry.bytes.length,
    httpEtag: '"test"',
    writeHttpMetadata: () => {},
  };
  return {
    objects,
    async get(key) { return object(objects.get(key)); },
    async head(key) { return object(objects.get(key)); },
    async put(key, body, options = {}) {
      const bytes = typeof body === 'string' ? new TextEncoder().encode(body) : new Uint8Array(await new Response(body).arrayBuffer());
      const digest = createHash('sha256').update(bytes).digest('hex');
      if (options.sha256 && options.sha256 !== digest) throw new Error('R2 digest mismatch');
      objects.set(key, { bytes, digest });
    },
  };
}

function upstream(version, failAsset = null) {
  globalThis.fetch = async (url) => {
    const path = new URL(url).pathname;
    if (path.endsWith('/channels/latest')) return new Response(JSON.stringify(release(version)));
    const name = path.split('/').at(-1);
    if (name === failAsset) return new Response('unavailable', { status: 503 });
    return new Response(`${version}:${name}`);
  };
}

test('hourly checks retain a complete latest release and expose status', async () => {
  const originalFetch = globalThis.fetch;
  try {
    const PACKAGES = bucket();
    upstream('0.160.0');
    let status = await syncAndRecord({ PACKAGES });
    assert.equal(status.state, 'ok');
    assert.equal(status.changed, true);
    assert.equal(status.cached_version, '0.160.0');
    const channelBefore = await (await PACKAGES.get('channels/latest')).text();

    status = await syncAndRecord({ PACKAGES });
    assert.equal(status.changed, false);
    assert.equal(await (await PACKAGES.get('channels/latest')).text(), channelBefore);

    PACKAGES.objects.delete('releases/0.160.0/codex-package_SHA256SUMS');
    status = await syncAndRecord({ PACKAGES });
    assert.equal(status.changed, true);
    assert.ok(await PACKAGES.head('releases/0.160.0/codex-package_SHA256SUMS'));

    upstream('0.161.0', names[1]);
    await assert.rejects(syncAndRecord({ PACKAGES }), /Official source returned 503/);
    assert.equal(await (await PACKAGES.get('channels/latest')).text(), channelBefore);
    const response = await worker.fetch(new Request('https://example.com/codex-cache/sync-status.json'), { PACKAGES });
    assert.equal(response.status, 200);
    assert.equal(response.headers.get('Cache-Control'), 'no-store');
    status = await response.json();
    assert.equal(status.state, 'error');
    assert.equal(status.upstream_version, '0.161.0');
    assert.equal(status.cached_version, '0.160.0');
    assert.ok(status.last_success_at);

    upstream('0.161.0');
    status = await syncAndRecord({ PACKAGES });
    assert.equal(status.state, 'ok');
    assert.equal(status.cached_version, '0.161.0');
    assert.equal(JSON.parse(await (await PACKAGES.get('channels/latest')).text()).tag_name, 'rust-v0.161.0');
  } finally {
    globalThis.fetch = originalFetch;
  }
});
