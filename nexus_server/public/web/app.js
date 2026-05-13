/**
 * Phaze Web preview — WebSocket smoke test only.
 * Protocol: https://github.com/jakes1345/skype7-reborn/blob/master/docs/NEXUS_PROTOCOL.md
 */
(function () {
  const logEl = document.getElementById('log');
  const wsInput = document.getElementById('wsurl');
  const userInput = document.getElementById('user');
  const passInput = document.getElementById('pass');
  const totpInput = document.getElementById('totp');
  const btnConnect = document.getElementById('btnConnect');
  const btnAuth = document.getElementById('btnAuth');
  const btnPing = document.getElementById('btnPing');

  let ws = null;

  function log(line) {
    const t = new Date().toISOString();
    logEl.textContent += `[${t}] ${line}\n`;
    logEl.scrollTop = logEl.scrollHeight;
  }

  function defaultWS() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${proto}//${window.location.host}/ws`;
  }
  if (!wsInput.value) wsInput.value = defaultWS();

  btnConnect.addEventListener('click', () => {
    if (ws) {
      try { ws.close(); } catch (_) {}
      ws = null;
    }
    const url = wsInput.value.trim();
    log('Connecting to ' + url + ' …');
    ws = new WebSocket(url);
    ws.addEventListener('open', () => {
      log('open');
      btnAuth.disabled = false;
      btnPing.disabled = false;
    });
    ws.addEventListener('message', (ev) => {
      log('<< ' + ev.data);
    });
    ws.addEventListener('close', (ev) => {
      log('close code=' + ev.code + ' reason=' + (ev.reason || ''));
      btnAuth.disabled = true;
      btnPing.disabled = true;
    });
    ws.addEventListener('error', () => log('error (see browser devtools)'));
  });

  btnAuth.addEventListener('click', () => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const device = 'phaze-web-preview';
    const msg = {
      type: 'auth',
      sender: userInput.value.trim(),
      body: passInput.value,
      totp_code: totpInput.value.trim(),
      device_info: device,
    };
    const line = JSON.stringify(msg);
    log('>> ' + line);
    ws.send(line);
  });

  btnPing.addEventListener('click', () => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const msg = { type: 'presence', body: 'web-preview' };
    const line = JSON.stringify(msg);
    log('>> ' + line);
    ws.send(line);
  });
})();
