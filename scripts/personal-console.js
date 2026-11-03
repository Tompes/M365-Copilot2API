(() => {
  "use strict";
  // Run ONLY in your own Microsoft Copilot tab. Nothing is uploaded by this script.
  if (!['m365.cloud.microsoft', 'copilot.com'].includes(location.hostname) || location.protocol !== 'https:') {
    console.error('请在 https://m365.cloud.microsoft/chat 的 Console 中运行此脚本。');
    return;
  }
  window.__m365PersonalCaptureStop?.();
  const original = { WebSocket: window.WebSocket, socketSend: window.WebSocket.prototype.send, fetch: window.fetch, open: XMLHttpRequest.prototype.open, send: XMLHttpRequest.prototype.send };
  const candidates = [];
  let stopped = false;
  let timer;
  let pendingSocket;
  const personalTenant = '84df9e7f-e9f6-40af-b435-aaaaaaaaaaaa';
  const cleanup = () => {
    if (stopped) return;
    stopped = true;
    clearTimeout(timer);
    if (window.WebSocket === CapturingSocket) window.WebSocket = original.WebSocket;
    if (original.WebSocket.prototype.send === capturingSocketSend) original.WebSocket.prototype.send = original.socketSend;
    if (window.fetch === capturingFetch) window.fetch = original.fetch;
    if (XMLHttpRequest.prototype.open === capturingOpen) XMLHttpRequest.prototype.open = original.open;
    if (XMLHttpRequest.prototype.send === capturingSend) XMLHttpRequest.prototype.send = original.send;
    if (window.__m365PersonalCaptureStop === cleanup) delete window.__m365PersonalCaptureStop;
    candidates.length = 0;
    pendingSocket = undefined;
  };
  const tokenURL = (raw) => {
    try {
      const u = new URL(raw, location.href);
      return u.hostname === 'login.microsoftonline.com' && u.pathname.endsWith('/oauth2/v2.0/token');
    } catch { return false; }
  };
  const remember = (body) => {
    if (!stopped && typeof body?.access_token === 'string') {
      candidates.push(body);
      if (candidates.length > 12) candidates.shift();
    }
  };
  async function capturingFetch(...args) {
    const response = await Reflect.apply(original.fetch, this, args);
    const raw = typeof args[0] === 'string' || args[0] instanceof URL ? String(args[0]) : args[0]?.url;
    if (tokenURL(raw) && response.ok) {
      response.clone().json().then(remember).catch(() => {});
    }
    return response;
  }
  const requestURLs = new WeakMap();
  function capturingOpen(method, url, ...rest) {
    requestURLs.set(this, String(url));
    return Reflect.apply(original.open, this, [method, url, ...rest]);
  }
  function capturingSend(...args) {
    if (tokenURL(requestURLs.get(this))) {
      this.addEventListener('load', () => {
        try {
          if (this.status === 200) remember(this.responseType === 'json' ? this.response : JSON.parse(this.responseText));
        } catch { /* A non-JSON response is not a credential export. */ }
      }, { once: true });
    }
    return Reflect.apply(original.send, this, args);
  }
  function capture(raw) {
    if (stopped || pendingSocket) return;
    let u;
    try { u = new URL(String(raw)); } catch { return; }
    if (u.protocol !== 'wss:' || u.hostname !== 'substrate.office.com') return;
    const match = u.pathname.match(/^\/m365Copilot\/Chathub\/([0-9a-f-]{36})@([0-9a-f-]{36})\/?$/i);
    const token = u.searchParams.get('access_token');
    if (!match || match[2].toLowerCase() !== personalTenant || !token) return;
    pendingSocket = true;
    // Allow the cloned OAuth response to finish parsing before restoring hooks.
    setTimeout(() => {
      if (stopped) return;
      const response = candidates.findLast((x) => x.access_token === token);
      const plan = u.searchParams.get('licenseType');
      const credentials = {
        access_token: token,
        oid: match[1],
        expires_at: Math.floor(Date.now() / 1000) + 1800,
        license_type: plan === 'Premium' ? 'Premium' : 'Free',
      };
      if (response?.refresh_token) credentials.refresh_token = response.refresh_token;
      // Conservative expiry is used when the browser reuses a cached token.
      const json = JSON.stringify(credentials, null, 2);
      cleanup();
      console.info('采集完成：将下面的 JSON 粘贴到你自己的网关「个人账号」输入框。不要把凭证发给他人。');
      console.log(json);
      if (typeof copy === 'function') {
        try { copy(json); console.info('JSON 已复制到剪贴板。'); } catch { /* Copy the output manually. */ }
      }
      if (!credentials.refresh_token) console.info('未捕获匹配的刷新令牌，本次凭证到期后需重新采集。');
    }, 300);
  }
  function capturingSocketSend(...args) {
    const result = Reflect.apply(original.socketSend, this, args);
    capture(this.url);
    return result;
  }
  class CapturingSocket extends original.WebSocket {
    constructor(...args) { super(...args); capture(args[0]); }
  }
  original.WebSocket.prototype.send = capturingSocketSend;
  window.WebSocket = CapturingSocket;
  window.fetch = capturingFetch;
  XMLHttpRequest.prototype.open = capturingOpen;
  XMLHttpRequest.prototype.send = capturingSend;
  window.__m365PersonalCaptureStop = cleanup;
  timer = setTimeout(() => {
    cleanup();
    console.info('采集已超时。请重新运行脚本，然后新建聊天并发送一条消息。');
  }, 5 * 60 * 1000);
  console.info('已开始采集。请勿刷新页面；新建聊天并发送一条简短消息。成功后会输出 JSON 并恢复浏览器接口。');
})();
