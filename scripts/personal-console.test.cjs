const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const source = fs.readFileSync(__dirname + '/personal-console.js', 'utf8');

function fixture(host = 'm365.cloud.microsoft') {
  const messages = [];
  const copied = [];
  const timers = new Map();
  let clock = 0;
  class Socket { static OPEN = 1; constructor(...args) { this.args = args; this.url = args[0]; } send(value) { this.sent = value; return "sent"; } }
  class XHR {
    open() {}
    send() {}
    addEventListener(event, handler) { this.listener = handler; }
  }
  let responseBody = {};
  const fetch = async () => ({ ok: true, clone: () => ({ json: async () => responseBody }) });
  const context = { URL, WeakMap, Date, JSON, Reflect, XMLHttpRequest: XHR,
    location: { hostname: host, protocol: 'https:', href: 'https://' + host + '/chat' },
    console: { info: (...a) => messages.push(a), log: (...a) => messages.push(a), error: (...a) => messages.push(a) },
    copy: (value) => copied.push(value),
    setTimeout: (callback, delay) => { const id = ++clock; timers.set(id, { callback, delay }); return id; },
    clearTimeout: (id) => timers.delete(id),
  };
  context.window = { WebSocket: Socket, fetch };
  vm.createContext(context);
  return { context, Socket, XHR, fetch, copied, messages, timers,
    run: () => vm.runInContext(source, context),
    response: (body) => { responseBody = body; },
    tick: (delay) => { for (const [id, item] of [...timers]) if (item.delay === delay) { timers.delete(id); item.callback(); } },
  };
}
const oid = '11111111-2222-4333-8444-555555555555';
const tenant = '84df9e7f-e9f6-40af-b435-aaaaaaaaaaaa';
const socketURL = (token = 'chat-secret', host = 'substrate.office.com', tid = tenant) => `wss://${host}/m365Copilot/Chathub/${oid}@${tid}?access_token=${token}&licenseType=Premium`;

test('refuses unrelated websites without installing hooks', () => {
  const f = fixture('example.com'); f.run();
  assert.equal(f.context.window.WebSocket, f.Socket);
  assert.equal(f.copied.length, 0);
});
test('exports actual personal socket token and restores all hooks', () => {
  const f = fixture(); f.run();
  assert.equal(f.context.window.WebSocket.OPEN, 1);
  new f.context.window.WebSocket(socketURL(), ['protocol']);
  f.tick(300);
  const output = JSON.parse(f.copied[0]);
  assert.equal(output.access_token, 'chat-secret');
  assert.equal(output.oid, oid);
  assert.equal(output.license_type, 'Premium');
  assert.equal(output.refresh_token, undefined);
  assert.ok(output.expires_at > Date.now() / 1000);
  assert.equal(f.context.window.WebSocket, f.Socket);
  assert.equal(f.context.window.fetch, f.fetch);
  assert.equal(f.timers.size, 0);
});
test('ignores unrelated hosts, organization sockets and duplicate captures', () => {
  const f = fixture(); f.run();
  new f.context.window.WebSocket(socketURL('private', 'attacker.example'));
  new f.context.window.WebSocket(socketURL('private', 'substrate.office.com', '11111111-2222-4333-8444-555555555555'));
  assert.equal(f.copied.length, 0);
  new f.context.window.WebSocket(socketURL('first'));
  new f.context.window.WebSocket(socketURL('second'));
  f.tick(300);
  assert.equal(f.copied.length, 1);
  assert.equal(JSON.parse(f.copied[0]).access_token, 'first');
});
test('includes only a refresh token matching the actual chat access token', async () => {
  const f = fixture(); f.run();
  f.response({ access_token: 'unrelated', refresh_token: 'wrong' });
  await f.context.window.fetch('https://login.microsoftonline.com/common/oauth2/v2.0/token');
  await new Promise(setImmediate);
  f.response({ access_token: 'chat-secret', refresh_token: 'correct' });
  await f.context.window.fetch('https://login.microsoftonline.com/common/oauth2/v2.0/token');
  await new Promise(setImmediate);
  new f.context.window.WebSocket(socketURL()); f.tick(300);
  assert.equal(JSON.parse(f.copied[0]).refresh_token, 'correct');
});
test('XHR capture and timeout cleanup work without changing original send behavior', () => {
  const f = fixture(); f.run();
  const xhr = new f.XHR(); xhr.open('POST', 'https://login.microsoftonline.com/common/oauth2/v2.0/token'); xhr.send('form');
  xhr.status = 200; xhr.responseType = 'json'; xhr.response = { access_token: 'chat-secret', refresh_token: 'xhr-refresh' }; xhr.listener();
  new f.context.window.WebSocket(socketURL()); f.tick(300);
  assert.equal(JSON.parse(f.copied[0]).refresh_token, 'xhr-refresh');
  const g = fixture(); g.run(); g.tick(300000);
  assert.equal(g.context.window.WebSocket, g.Socket);
  assert.equal(g.context.window.fetch, g.fetch);
});
test('running twice replaces the previous capture and does not stack wrappers', () => {
  const f = fixture(); f.run(); f.run();
  new f.context.window.WebSocket(socketURL()); f.tick(300);
  assert.equal(f.copied.length, 1);
  assert.equal(f.context.window.WebSocket, f.Socket);
});

test('captures an existing socket even if the app cached the native constructor', () => {
  const f = fixture(); const existing = new f.Socket(socketURL()); f.run();
  assert.equal(existing.send('chat-frame'), 'sent'); f.tick(300);
  assert.equal(JSON.parse(f.copied[0]).access_token, 'chat-secret');
  assert.equal(existing.sent, 'chat-frame');
});
