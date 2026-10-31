import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

async function screen({ picker = false, secure = true, search = '' } = {}) {
  const html = await readFile(new URL('./index.html', import.meta.url), 'utf8');
  const elements = new Map([...html.matchAll(/\bid="([^"]+)"/g)].map(([,id]) => [id, {hidden:false,disabled:false,textContent:'',className:'',addEventListener(){}}]));
  const previous = { window: globalThis.window, document: globalThis.document, location: globalThis.location, fetch: globalThis.fetch };
  globalThis.window = { isSecureContext: secure, ...(picker ? { showDirectoryPicker() {} } : {}) };
  globalThis.location = { search };
  globalThis.document = { getElementById(id) {
    return elements.get(id) ?? null;
  } };
  globalThis.fetch = async () => new Response(JSON.stringify({version:'0.1.8',tailscale:'1.104.4',size:1,sha256:'a'.repeat(64),file:'KoboRoot.tgz'}));
  try { await import('./main.mjs?case='+Math.random()); }
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
