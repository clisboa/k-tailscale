import { test } from 'node:test';
import assert from 'node:assert/strict';
import { webcrypto } from 'node:crypto';
import { readKobo, installPackage } from './drive.mjs';

const notFound = () => Object.assign(new Error('Missing'), { name: 'NotFoundError' });
class FakeFile {
  constructor(bytes) { this.bytes = new Uint8Array(bytes); this.failWrite = false; }
  async getFile() { return new File([this.bytes], 'fixture', { lastModified: 1 }); }
  async isSameEntry(other) { return this === other; }
  async createWritable(options) {
    assert.equal(options.mode, 'exclusive');
    let pending = this.bytes;
    return { write: async bytes => { if (this.failWrite) throw new Error('Drive unavailable'); pending = bytes; },
      close: async () => { this.bytes = new Uint8Array(pending); }, abort: async () => {} };
  }
}
class FakeDirectory {
  constructor() { this.files = new Map(); this.directories = new Map(); }
  async getDirectoryHandle(name) { if (!this.directories.has(name)) throw notFound(); return this.directories.get(name); }
  async getFileHandle(name, options = {}) {
    if (!this.files.has(name) && options.create) this.files.set(name, new FakeFile([]));
    if (!this.files.has(name)) throw notFound();
    return this.files.get(name);
  }
}
function kobo(version = 'a,b,4.46.23852,d,e,00000000-0000-0000-0000-000000000390') {
  const root = new FakeDirectory(), hidden = new FakeDirectory();
  hidden.files.set('version', new FakeFile(new TextEncoder().encode(version)));
  root.directories.set('.kobo', hidden);
  return { root, hidden };
}
async function release(bytes = new Uint8Array([1, 2, 3])) {
  const sha256 = Buffer.from(await webcrypto.subtle.digest('SHA-256', bytes)).toString('hex');
  return { bytes, manifest: { version: '0.1.0', tailscale: '1.104.4', size: bytes.length, sha256, file: 'KoboRoot.tgz' } };
}

test('detect supported Kobo without retaining its serial', async () => {
  const { root } = kobo(); const device = await readKobo(root);
  assert.equal(device.model, 'Kobo Libra Colour'); assert.equal(device.firmware, '4.46.23852');
  assert.equal(JSON.stringify(device).includes('00000000-'), false);
});
test('reject wrong drive, model and firmware', async () => {
  await assert.rejects(readKobo(new FakeDirectory()));
  for (const version of ['a,b,4.99.0,d,e,390', 'a,b,4.46.23852,d,e,393']) await assert.rejects(readKobo(kobo(version).root));
});
test('copy only verified package bytes and verify readback', async () => {
  const { root, hidden } = kobo(); const { bytes, manifest } = await release();
  await installPackage(await readKobo(root), manifest, { fetcher: async () => new Response(bytes), crypto: webcrypto });
  assert.deepEqual(hidden.files.get('KoboRoot.tgz').bytes, bytes);
});
test('preserve any existing pending update', async () => {
  const { root, hidden } = kobo(); const pending = new FakeFile([9]); hidden.files.set('KoboRoot.tgz', pending);
  const { manifest } = await release(); let downloads = 0;
  await assert.rejects(installPackage(await readKobo(root), manifest, { fetcher: async () => { downloads++; }, crypto: webcrypto }));
  assert.equal(downloads, 0); assert.deepEqual(pending.bytes, new Uint8Array([9]));
});
test('recheck pending update after download and before creating a writer', async () => {
  const { root, hidden } = kobo(); const { bytes, manifest } = await release(); const pending = new FakeFile([8]);
  await assert.rejects(installPackage(await readKobo(root), manifest, { fetcher: async () => { hidden.files.set('KoboRoot.tgz', pending); return new Response(bytes); }, crypto: webcrypto }));
  assert.deepEqual(pending.bytes, new Uint8Array([8]));
});
test('reject altered, oversized and incomplete downloads before opening a file', async () => {
  for (const bytes of [new Uint8Array([1, 2]), new Uint8Array([1, 2, 3, 4]), new Uint8Array([4, 5, 6])]) {
    const { root, hidden } = kobo(); const { manifest } = await release();
    await assert.rejects(installPackage(await readKobo(root), manifest, { fetcher: async () => new Response(bytes), crypto: webcrypto }));
    assert.equal(hidden.files.has('KoboRoot.tgz'), false);
  }
});
test('write failure preserves the entry rather than deleting an uncertain pending update', async () => {
  const { root, hidden } = kobo(); const { bytes, manifest } = await release();
  const get = hidden.getFileHandle.bind(hidden);
  hidden.getFileHandle = async (name, options) => { const file = await get(name, options); if (name === 'KoboRoot.tgz') file.failWrite = true; return file; };
  await assert.rejects(installPackage(await readKobo(root), manifest, { fetcher: async () => new Response(bytes), crypto: webcrypto }));
  assert.equal(hidden.files.has('KoboRoot.tgz'), true);
});
test('readback mismatch is reported without deleting an uncertain file', async () => {
  const { root, hidden } = kobo(); const { bytes, manifest } = await release();
  const get = hidden.getFileHandle.bind(hidden);
  hidden.getFileHandle = async (name, options) => {
    const file = await get(name, options);
    if (name === 'KoboRoot.tgz') {
      const writer = file.createWritable.bind(file);
      file.createWritable = async options => { const stream = await writer(options); const close = stream.close; stream.close = async () => { await close(); file.bytes = new Uint8Array([9]); }; return stream; };
    }
    return file;
  };
  await assert.rejects(installPackage(await readKobo(root), manifest, { fetcher: async () => new Response(bytes), crypto: webcrypto }));
  assert.equal(hidden.files.has('KoboRoot.tgz'), true);
});
test('cancellation after download never creates an update file', async () => {
  const { root, hidden } = kobo(); const { bytes, manifest } = await release(); const controller = new AbortController();
  await assert.rejects(installPackage(await readKobo(root), manifest, { fetcher: async () => new Response(bytes), crypto: webcrypto,
    signal: controller.signal, onProgress: () => controller.abort() }));
  assert.equal(hidden.files.has('KoboRoot.tgz'), false);
});
