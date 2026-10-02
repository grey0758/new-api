const UPSTREAM = 'https://releases.openai.com/codex';
const TARGETS = [
  'x86_64-unknown-linux-musl',
  'aarch64-unknown-linux-musl',
  'x86_64-apple-darwin',
  'aarch64-apple-darwin',
  'x86_64-pc-windows-msvc',
  'aarch64-pc-windows-msvc',
];
const PACKAGE_NAMES = TARGETS.map((target) => `codex-package-${target}.tar.gz`);
const REQUIRED = [...PACKAGE_NAMES, 'codex-package_SHA256SUMS'];
const SHA256 = /^sha256:([a-f0-9]{64})$/i;
const VERSION = /^rust-v(\d+\.\d+\.\d+(?:-(?:alpha|beta)(?:\.\d+){0,2})?)$/;

async function fetchOfficial(url) {
  const response = await fetch(url, { headers: { 'User-Agent': 'opencodex-codex-release-cache/1' } });
  if (!response.ok || !response.body) throw new Error(`Official source returned ${response.status} for ${new URL(url).pathname}`);
  return response;
}

async function update(env) {
  const officialResponse = await fetchOfficial(`${UPSTREAM}/channels/latest?checked_at=${Date.now()}`);
  const text = await officialResponse.text();
  const release = JSON.parse(text);
  const match = VERSION.exec(release.tag_name || '');
  if (!match || !Array.isArray(release.assets)) throw new Error('Invalid official release metadata');
  const version = match[1];
  const current = await env.PACKAGES.get('channels/latest');
  if (current) {
    const previous = JSON.parse(await current.text());
    if (previous.tag_name === release.tag_name) return { version, changed: false };
  }
  const assets = new Map(release.assets.map((asset) => [asset.name, asset]));
  for (const name of REQUIRED) {
    const asset = assets.get(name);
    const digest = SHA256.exec(asset?.digest || '');
    if (!digest) throw new Error(`Missing SHA-256 for ${name}`);
    const key = `releases/${version}/${name}`;
    const existing = await env.PACKAGES.head(key);
    if (existing?.checksums?.sha256?.toLowerCase() === digest[1].toLowerCase()) continue;
    const response = await fetchOfficial(`${UPSTREAM}/${key}`);
    await env.PACKAGES.put(key, response.body, {
      sha256: digest[1],
      httpMetadata: { contentType: name.endsWith('.tar.gz') ? 'application/gzip' : 'text/plain; charset=utf-8' },
      customMetadata: { source: 'releases.openai.com', version },
    });
  }
  // Publish only after every archive and checksum has reached R2.
  const metadata = { httpMetadata: { contentType: 'application/json; charset=utf-8', cacheControl: 'no-cache' } };
  await env.PACKAGES.put(`releases/${version}/release.json`, text, metadata);
  await env.PACKAGES.put('channels/latest', text, metadata);
  return { version, changed: true };
}

async function serve(request, env) {
  if (request.method !== 'GET' && request.method !== 'HEAD') return new Response('Method not allowed', { status: 405 });
  const url = new URL(request.url);
  let key = url.pathname.replace(/^\/codex-cache\/?/, '');
  if (key === 'install.sh') {
    const object = await env.PACKAGES.get('install.sh');
    if (!object) return new Response('Installer unavailable', { status: 503 });
    return new Response(request.method === 'HEAD' ? null : object.body, {
      headers: { 'Content-Type': 'text/x-shellscript; charset=utf-8', 'Cache-Control': 'public, max-age=300', 'X-Codex-Cache': 'r2' },
    });
  }
  if (!/^channels\/latest$/.test(key) && !/^releases\/[0-9]+\.[0-9]+\.[0-9]+(?:-(?:alpha|beta)(?:\.[0-9]+){0,2})?\/(?:release\.json|codex-package_(?:SHA256SUMS)|codex-package-(?:x86_64|aarch64)-(?:unknown-linux-musl|apple-darwin|pc-windows-msvc)\.tar\.gz)$/.test(key)) {
    return new Response('Not found', { status: 404 });
  }
  const object = request.method === 'HEAD'
    ? await env.PACKAGES.head(key)
    : request.headers.has('Range')
      ? await env.PACKAGES.get(key, { range: request.headers })
      : await env.PACKAGES.get(key);
  if (!object) return new Response('Not found', { status: 404 });
  const headers = new Headers();
  object.writeHttpMetadata(headers);
  headers.set('ETag', object.httpEtag);
  headers.set('Accept-Ranges', 'bytes');
  headers.set('X-Codex-Cache', 'r2');
  headers.set('Cache-Control', key === 'channels/latest' ? 'public, max-age=300' : 'public, max-age=31536000, immutable');
  let status = 200;
  if (request.headers.has('Range') && object.range && 'offset' in object.range) {
    status = 206;
    headers.set('Content-Range', `bytes ${object.range.offset}-${object.range.offset + object.range.length - 1}/${object.size}`);
    headers.set('Content-Length', String(object.range.length));
  } else headers.set('Content-Length', String(object.size));
  return new Response(request.method === 'HEAD' ? null : object.body, { status, headers });
}

export default {
  async fetch(request, env) {
    try { return await serve(request, env); }
    catch (error) { console.error('cache serve failed', String(error)); return new Response('Cache unavailable', { status: 503 }); }
  },
  async scheduled(_event, env, ctx) {
    ctx.waitUntil(update(env).then((result) => console.log('release sync', result.version, result.changed)).catch((error) => console.error('release sync failed', String(error))));
  },
};

export { update };
