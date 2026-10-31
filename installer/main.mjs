import { readKobo, loadManifest, installPackage } from './drive.mjs';

const $ = id => document.getElementById(id);
let device = null, manifest = null, busy = false;
const manual = new URLSearchParams(location.search).has('manual');
const guided = typeof window.showDirectoryPicker === 'function' && window.isSecureContext && !manual;
function message(id, text, kind = '') { $(id).textContent = text; $(id).className = 'status '+kind; }
function ready() { $('install').disabled = busy || !device || !manifest; $('select').disabled = busy; }

if (!guided) {
  $('step-select').hidden = true; $('step-install').hidden = true; $('step-finish').hidden = true; $('fallback').hidden = false;
  $('fallback-reason').textContent = manual
    ? 'You chose manual installation. Download the package below and copy it to your Kobo.'
    : !window.isSecureContext
      ? 'Guided folder access needs a secure page. Open the HTTPS installer, or download and copy the package below.'
      : 'This browser does not provide guided folder access. This is unrelated to whether your Kobo is connected. Use Google Chrome or Microsoft Edge for guided installation, or download and copy the package below.';
}

async function release() {
  try {
    manifest = await loadManifest();
    $('release').textContent = `k-tailscale ${manifest.version} · Initial Tailscale ${manifest.tailscale}`;
  } catch (error) { message('status', error.message, 'error'); $('release').textContent = 'Release information is unavailable. Reload to retry.'; }
  ready();
}

$('select').addEventListener('click', async () => {
  try {
    const root = await window.showDirectoryPicker({ id: 'k-tailscale-kobo', mode: 'readwrite' });
    device = await readKobo(root);
    $('select').textContent = 'Kobo drive selected ✓';
    message('device', `${device.model} · Firmware ${device.firmware}`, 'success');
    $('step-connect').classList.add('done'); $('step-select').classList.add('done');
    message('status', '');
  } catch (error) {
    device = null; $('select').textContent = 'Select Kobo drive →';
    message('device', error.name === 'AbortError'
      ? 'Folder selection was cancelled. No files were written. Select the KOBOeReader drive to try again.'
      : error.message, error.name === 'AbortError' ? '' : 'error');
  }
  ready();
});

$('install').addEventListener('click', async () => {
  if (busy || !device || !manifest) return;
  busy = true; ready(); $('progress').hidden = false;
  message('status', 'Downloading the verified installer…');
  try {
    const work = () => installPackage(device, manifest, { onProgress: (done, total) => {
      $('progress').value = done / total * 100;
      message('status', done === total ? 'Checking and copying…' : `Downloading… ${Math.floor(done / total * 100)}%`);
    } });
    if (navigator.locks) await navigator.locks.request('k-tailscale-install', work); else await work();
    message('status', 'Copied and verified. Safely eject your Kobo, then unplug it.', 'success');
    $('step-install').classList.add('done'); $('step-finish').classList.add('done');
    $('install').textContent = 'Copied and verified ✓';
  } catch (error) { message('status', `${error.message} Keep the Kobo connected until you resolve any pending update.`, 'error'); busy = false; ready(); }
  $('progress').hidden = true;
});

await release();
