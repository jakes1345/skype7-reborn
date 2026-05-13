/**
 * Phaze Web pilot — WebSocket + NaCl box (tweetnacl) matching native E2EE: prefix.
 * Requires vendor/nacl-fast.min.js (sets global `nacl`).
 */
(function () {
  const logEl = document.getElementById('log');
  const wsInput = document.getElementById('wsurl');
  const userInput = document.getElementById('user');
  const passInput = document.getElementById('pass');
  const totpInput = document.getElementById('totp');
  const btnConnect = document.getElementById('btnConnect');
  const btnAuth = document.getElementById('btnAuth');
  const panelLogin = document.getElementById('panelLogin');
  const panelChat = document.getElementById('panelChat');
  const whoami = document.getElementById('whoami');
  const myfpEl = document.getElementById('myfp');
  const peerInput = document.getElementById('peer');
  const draftInput = document.getElementById('draft');
  const btnSend = document.getElementById('btnSend');
  const btnLogout = document.getElementById('btnLogout');
  const btnKeyReq = document.getElementById('btnKeyReq');
  const btnAnnounce = document.getElementById('btnAnnounce');
  const threadEl = document.getElementById('thread');

  if (typeof nacl === 'undefined') {
    logEl.textContent = 'FATAL: nacl not loaded. Check vendor/nacl-fast.min.js\n';
    return;
  }

  let ws = null;
  let username = '';
  /** @type {Uint8Array|null} */
  let mySecret = null;
  /** @type {Uint8Array|null} */
  let myPublic = null;
  /** @type {Record<string, Uint8Array>} */
  const peerPub = Object.create(null);
  /** @type {Record<string, string>} fingerprint TOFU */
  const peerFp = Object.create(null);

  function log(line) {
    const t = new Date().toISOString();
    logEl.textContent += `[${t}] ${line}\n`;
    logEl.scrollTop = logEl.scrollHeight;
  }

  function bytesToHex(u8) {
    return Array.from(u8, (b) => b.toString(16).padStart(2, '0')).join('');
  }

  function hexToBytes(hex) {
    const clean = hex.replace(/^0x/i, '');
    if (clean.length % 2 !== 0) throw new Error('bad hex length');
    const out = new Uint8Array(clean.length / 2);
    for (let i = 0; i < out.length; i++) {
      out[i] = parseInt(clean.slice(i * 2, i * 2 + 2), 16);
    }
    return out;
  }

  function u8ToBase64(u8) {
    let s = '';
    for (let i = 0; i < u8.length; i++) s += String.fromCharCode(u8[i]);
    return btoa(s);
  }

  function base64ToU8(b64) {
    const bin = atob(b64);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  async function fingerprintPub(pub32) {
    const h = await crypto.subtle.digest('SHA-256', pub32.buffer.slice(pub32.byteOffset, pub32.byteOffset + pub32.byteLength));
    return bytesToHex(new Uint8Array(h).subarray(0, 8));
  }

  function lsKeysKey(u) {
    return `phaze.web.keys.v1:${location.origin}:${u}`;
  }

  function lsPinKey(peer) {
    return `phaze.web.pin.v1:${location.origin}:${username}:${peer}`;
  }

  function loadOrCreateKeypair(u) {
    const raw = localStorage.getItem(lsKeysKey(u));
    if (raw) {
      const j = JSON.parse(raw);
      return { publicKey: hexToBytes(j.publicKey), secretKey: hexToBytes(j.secretKey) };
    }
    const kp = nacl.box.keyPair();
    localStorage.setItem(
      lsKeysKey(u),
      JSON.stringify({ publicKey: bytesToHex(kp.publicKey), secretKey: bytesToHex(kp.secretKey) }),
    );
    return kp;
  }

  function encryptForPeer(plain, recipientPub32) {
    const msg = new TextEncoder().encode(plain);
    const nonce = nacl.randomBytes(24);
    const boxed = nacl.box(msg, nonce, recipientPub32, mySecret);
    const combined = new Uint8Array(24 + boxed.length);
    combined.set(nonce, 0);
    combined.set(boxed, 24);
    return `E2EE:${bytesToHex(combined)}`;
  }

  function decryptFromPeer(body, sender) {
    if (!body || !body.startsWith('E2EE:')) return body;
    const pk = peerPub[sender];
    if (!pk || !mySecret) return '[no peer key]';
    let raw;
    try {
      raw = hexToBytes(body.slice(5));
    } catch {
      return '[bad hex]';
    }
    if (raw.length < 24) return '[short cipher]';
    const nonce = raw.subarray(0, 24);
    const sealed = raw.subarray(24);
    const opened = nacl.box.open(sealed, nonce, pk, mySecret);
    if (!opened) return '[decrypt failed]';
    return new TextDecoder().decode(opened);
  }

  function normalizePubField(v) {
    if (!v) return null;
    if (typeof v === 'string') {
      try {
        return base64ToU8(v);
      } catch {
        return null;
      }
    }
    if (Array.isArray(v)) return new Uint8Array(v);
    return null;
  }

  function rememberPeerKey(peer, pub32, fpHint) {
    if (!pub32 || pub32.length !== 32) return;
    fingerprintPub(pub32).then((fp) => {
      const stored = localStorage.getItem(lsPinKey(peer));
      if (stored && stored !== fp) {
        log(`SECURITY: ${peer} key fingerprint changed (stored ${stored}, now ${fp}) — ignoring`);
        return;
      }
      if (!stored && fpHint && fpHint !== fp) {
        log(`WARNING: ${peer} server hint fp ${fpHint} != computed ${fp} — still pinning computed`);
      }
      if (!stored) localStorage.setItem(lsPinKey(peer), fp);
      peerPub[peer] = pub32;
      peerFp[peer] = fp;
      log(`Pinned key for ${peer} (fp ${fp})`);
    });
  }

  function send(obj) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const line = JSON.stringify(obj);
    log('>> ' + line);
    ws.send(line);
  }

  function announcePresence(status) {
    if (!myPublic || !username) return;
    fingerprintPub(myPublic).then((fp) => {
      send({
        type: 'presence',
        status: status || 'Online',
        public_key: u8ToBase64(myPublic),
        key_fingerprint: fp,
      });
    });
  }

  function appendThread(dir, who, text) {
    const wrap = document.createElement('div');
    wrap.className = 'bubble ' + (dir === 'out' ? 'out' : 'in');
    const body = document.createElement('div');
    body.textContent = text;
    const meta = document.createElement('div');
    meta.className = 'meta';
    meta.textContent = who + ' · ' + new Date().toLocaleTimeString();
    wrap.appendChild(body);
    wrap.appendChild(meta);
    threadEl.appendChild(wrap);
    threadEl.scrollTop = threadEl.scrollHeight;
  }

  function defaultWS() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${proto}//${window.location.host}/ws`;
  }
  if (!wsInput.value) wsInput.value = defaultWS();

  btnConnect.addEventListener('click', () => {
    if (ws) {
      try {
        ws.close();
      } catch (_) {}
      ws = null;
    }
    const url = wsInput.value.trim();
    log('Connecting to ' + url + ' …');
    ws = new WebSocket(url);
    ws.addEventListener('open', () => {
      log('open');
      btnAuth.disabled = false;
    });
    ws.addEventListener('message', (ev) => {
      log('<< ' + ev.data);
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      handleInbound(msg);
    });
    ws.addEventListener('close', (ev) => {
      log('close code=' + ev.code + ' reason=' + (ev.reason || ''));
      btnAuth.disabled = true;
      panelChat.style.display = 'none';
      panelLogin.style.display = 'block';
    });
    ws.addEventListener('error', () => log('error (see browser devtools)'));
  });

  function handleInbound(msg) {
    if (msg.type === 'auth_result' && msg.status === 'ok') {
      username = msg.sender || userInput.value.trim();
      const kp = loadOrCreateKeypair(username);
      mySecret = kp.secretKey;
      myPublic = kp.publicKey;
      whoami.textContent = username;
      fingerprintPub(myPublic).then((fp) => {
        myfpEl.textContent = fp;
      });
      panelLogin.style.display = 'none';
      panelChat.style.display = 'block';
      announcePresence('Online');
      appendThread('in', 'system', 'Connected. Add a peer name and exchange keys.');
      return;
    }

    if (msg.type === 'presence' && msg.sender && msg.sender !== username) {
      const pk = normalizePubField(msg.public_key);
      if (pk && pk.length === 32) {
        rememberPeerKey(msg.sender, pk, msg.key_fingerprint || '');
      }
    }

    if (msg.type === 'msg' && msg.sender && msg.sender !== username) {
      const clear = decryptFromPeer(msg.body, msg.sender);
      appendThread('in', msg.sender, clear);
    }
  }

  btnAuth.addEventListener('click', () => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    send({
      type: 'auth',
      sender: userInput.value.trim(),
      body: passInput.value,
      totp_code: totpInput.value.trim(),
      device_info: 'phaze-web-pilot',
    });
  });

  btnAnnounce.addEventListener('click', () => announcePresence('Online'));

  btnKeyReq.addEventListener('click', () => {
    const peer = peerInput.value.trim();
    if (!peer) return;
    send({ type: 'key_request', recipient: peer });
  });

  btnSend.addEventListener('click', () => {
    const peer = peerInput.value.trim();
    const text = draftInput.value.trim();
    if (!peer || !text) return;
    let body = text;
    if (peerPub[peer]) {
      try {
        body = encryptForPeer(text, peerPub[peer]);
      } catch (e) {
        log('encrypt error: ' + e);
        return;
      }
    } else {
      log('warn: no public key for ' + peer + ' — sending plaintext (not E2EE)');
    }
    send({ type: 'msg', recipient: peer, body });
    appendThread('out', username || 'me', peerPub[peer] ? text : text + ' (plaintext)');
    draftInput.value = '';
  });

  btnLogout.addEventListener('click', () => {
    if (ws) ws.close();
  });
})();
