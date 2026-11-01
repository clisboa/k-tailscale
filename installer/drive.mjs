const activeTargets = new WeakSet();
const versionPattern = /^(0|[1-9]\d{0,5})\.(0|[1-9]\d{0,5})\.(0|[1-9]\d{0,5})$/;
const pendingMessage = 'A KoboRoot.tgz update is already waiting on this Kobo. Eject and let it finish before installing k-tailscale.';
const supportedDevices = new Map([
  ['390', { model: 'Kobo Libra Colour', firmware: '4.46.23852' }],
  ['376', { model: 'Kobo Clara HD', firmware: '4.38.23852' }],
]);

export async function readKobo(root) {
  let directory, file;
  try {
    directory = await root.getDirectoryHandle('.kobo');
    file = await (await directory.getFileHandle('version')).getFile();
  } catch { throw new Error('Select the KOBOeReader drive itself. Its .kobo folder could not be found.'); }
  if (file.size > 4096) throw new Error('The selected drive has an invalid Kobo version file.');
  const fields = (await file.text()).trim().split(',');
  const model = fields[5]?.split('-').pop().replace(/^0+/, '');
  const device = supportedDevices.get(model);
  if (!device) throw new Error('This release supports the Kobo Libra Colour and Kobo Clara HD.');
  if (fields[2] !== device.firmware) throw new Error(`${device.model} requires firmware ${device.firmware}.`);
  return { directory, model: device.model, firmware: fields[2] };
}

export function validateManifest(value) {
  if (!value || !versionPattern.test(value.version) || !versionPattern.test(value.tailscale)
      || !Number.isSafeInteger(value.size) || value.size < 1 || value.size > 128 * 1024 * 1024
      || !/^[a-f0-9]{64}$/.test(value.sha256) || value.file !== 'KoboRoot.tgz') {
    throw new Error('The installer release information is invalid.');
  }
  return value;
}

async function pending(directory) {
  try { await directory.getFileHandle('KoboRoot.tgz'); return true; }
  catch (error) { if (error.name === 'NotFoundError') return false; throw error; }
}

async function readBytes(response, limit, signal, onProgress = () => {}) {
  if (!response.ok || !response.body) throw new Error('The installer could not be downloaded.');
  const reader = response.body.getReader(), chunks = [];
  let length = 0;
  try {
    while (true) {
      signal?.throwIfAborted();
      const { value, done } = await reader.read();
      if (done) break;
      length += value.length;
      if (length > limit) throw new Error('The download is larger than the verified release.');
      chunks.push(value); onProgress(length, limit);
    }
  } catch (error) { await reader.cancel().catch(() => {}); throw error; }
  finally { reader.releaseLock(); }
  const bytes = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
  return bytes;
}

async function hash(bytes, crypto) {
  return [...new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))].map(byte => byte.toString(16).padStart(2, '0')).join('');
}

export async function loadManifest(fetcher = fetch) {
  const response = await fetcher('install.json', { cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer' });
  const bytes = await readBytes(response, 64 * 1024);
  return validateManifest(JSON.parse(new TextDecoder().decode(bytes)));
}

export async function installPackage(device, release, options = {}) {
  const { fetcher = fetch, crypto = globalThis.crypto, signal, onProgress } = options;
  const directory = device.directory;
  validateManifest(release);
  if (activeTargets.has(directory)) throw new Error('An installation is already in progress.');
  activeTargets.add(directory);
  let stream;
  try {
    signal?.throwIfAborted();
    if (await pending(directory)) throw new Error(pendingMessage);
    const response = await fetcher(release.file, { cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer', signal });
    const bytes = await readBytes(response, release.size, signal, onProgress);
    if (bytes.length !== release.size || await hash(bytes, crypto) !== release.sha256) throw new Error('Download verification failed. Nothing was copied to the Kobo.');
    signal?.throwIfAborted();
    if (await pending(directory)) throw new Error(pendingMessage);
    const file = await directory.getFileHandle('KoboRoot.tgz', { create: true });
    if ((await file.getFile()).size !== 0) throw new Error(pendingMessage);
    stream = await file.createWritable({ mode: 'exclusive', keepExistingData: false });
    signal?.throwIfAborted();
    await stream.write(bytes); await stream.close(); stream = null;
    const copied = await file.getFile();
    if (copied.size !== release.size || await hash(await copied.arrayBuffer(), crypto) !== release.sha256) {
      throw new Error('The copied update could not be verified. Keep the Kobo connected and check its pending update before retrying.');
    }
  } catch (error) {
    if (stream) await stream.abort().catch(() => {});
    // The browser cannot prove exclusive creation against external writers.
    // Preserve any file on failure rather than risk deleting another update.
    throw error;
  } finally { activeTargets.delete(directory); }
}
