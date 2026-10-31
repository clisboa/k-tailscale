import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

async function screen({ picker = false, secure = true, search = '' } = {}, exercise) {
  const html = await readFile(new URL('./index.html', import.meta.url), 'utf8');
  const elements = new Map([...html.matchAll(/\bid="([^"]+)"/g)].map(([,id]) => [id, {hidden:false,disabled:false,textContent:'',className:'',handlers:{},classList:{add(){}},addEventListener(type,handler){this.handlers[type]=handler;}}]));
  const previous = { window: globalThis.window, document: globalThis.document, location: globalThis.location, fetch: globalThis.fetch };
  globalThis.window = { isSecureContext: secure, ...(picker ? { showDirectoryPicker: typeof picker === 'function' ? picker : () => {} } : {}) };
  globalThis.location = { search };
  globalThis.document = { getElementById(id) {
    return elements.get(id) ?? null;
  } };
  globalThis.fetch = async () => new Response(JSON.stringify({version:'0.1.8',tailscale:'1.104.4',size:1,sha256:'a'.repeat(64),file:'KoboRoot.tgz'}));
  try { await import('./main.mjs?case='+Math.random()); if (exercise) await exercise(elements); }
  finally { Object.assign(globalThis, previous); }
  return elements;
}

test('a browser without folder access explains the fallback independently of USB', async () => {
  const e = await screen();
  assert.equal(e.get('fallback').hidden, false);
  assert.equal(e.get('step-select').hidden, true);
  assert.match(e.get('fallback-reason')?.textContent ?? '', /browser.*folder access/i);
});
test('an explicit manual choice is identified rather than blaming browser support', async () => {
  const e = await screen({picker:true,search:'?manual=1'});
  assert.match(e.get('fallback-reason')?.textContent ?? '', /chose manual/i);
});
test('an insecure page explains how to obtain guided access', async () => {
  const e = await screen({picker:true,secure:false});
  assert.match(e.get('fallback-reason')?.textContent ?? '', /secure.*https/i);
});
test('a supported secure browser retains the guided controls', async () => {
  const e = await screen({picker:true});
  assert.equal(e.get('step-select').hidden, false);
  assert.equal(e.get('install').disabled, true);
});

function supportedDrive() {
  return { async getDirectoryHandle(name) {
    assert.equal(name, '.kobo');
    return { async getFileHandle(name) {
      assert.equal(name, 'version');
      return { async getFile() { return new File(['fixture,4.9.77,4.46.23852,d,e,00000000-0000-0000-0000-000000000390'], 'version'); } };
    } };
  } };
}
test('a selected supported drive is visibly confirmed and enables installation', async () => {
  await screen({picker:async () => supportedDrive()}, async e => {
    await e.get('select').handlers.click();
    assert.equal(e.get('install').disabled, false);
    assert.match(e.get('device').textContent, /Kobo Libra Colour/);
    assert.match(e.get('select').textContent, /selected/i);
  });
});
test('a cancelled folder selection gives visible feedback and cannot install', async () => {
  await screen({picker:async () => { throw Object.assign(new Error('Cancelled'), {name:'AbortError'}); }}, async e => {
    await e.get('select').handlers.click();
    assert.equal(e.get('install').disabled, true);
    assert.match(e.get('device').textContent, /cancel/i);
  });
});
