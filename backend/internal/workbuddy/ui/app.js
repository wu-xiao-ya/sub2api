'use strict';
const bridge = new URLSearchParams(location.hash.slice(1));
const token = bridge.get('bridge_token');
const pending = new Map();
let session = '';
let accounts = [];
const el = id => document.getElementById(id);
function request(type, payload = {}) {
  return new Promise((resolve, reject) => {
    const id = crypto.randomUUID();
    const timer = setTimeout(() => { pending.delete(id); reject(new Error('请求超时，请刷新状态后重试')); }, type === 'workbuddy.import' ? 60000 : 45000);
    pending.set(id, { resolve, reject, timer });
    parent.postMessage({ source: 'sub2api-plugin-ui', bridge_token: token, request_id: id, type, ...payload }, '*');
  });
}
addEventListener('message', event => {
  const data = event.data;
  if (event.source !== parent || !data || data.source !== 'sub2api-plugin-host' || data.bridge_token !== token) return;
  const item = pending.get(data.request_id);
  if (!item) return;
  clearTimeout(item.timer); pending.delete(data.request_id);
  if (data.ok) item.resolve(data.result ?? data.config);
  else item.reject(new Error(String(data.error ?? data.message ?? '操作失败')));
});
addEventListener('pagehide', () => { for (const item of pending.values()) { clearTimeout(item.timer); item.reject(new Error('页面已关闭')); } pending.clear(); });
function chosen() { const ref = el('identity').value; if (!ref) throw new Error('请先添加或选择账号'); return ref; }
async function status() {
  const result = await request('plugin.status');
  const snapshot = typeof result.status_json === 'string' ? JSON.parse(result.status_json || '{}') : result.status_json;
  accounts = snapshot?.accounts ?? [];
  const selected = el('identity').value; el('identity').replaceChildren();
  for (const account of accounts) { const option = document.createElement('option'); option.value = account.identity_ref; option.textContent = account.identity_ref; el('identity').append(option); }
  if (accounts.some(a => a.identity_ref === selected)) el('identity').value = selected;
  renderModels();
}
function renderModels() {
  const account = accounts.find(a => a.identity_ref === el('identity').value);
  el('models').textContent = (account?.models ?? []).join('\n');
  el('identity-status').textContent = !account ? '尚未保存授权' : account.expires_at > 0
    ? `已保存授权；凭据到期时间 ${new Date(account.expires_at * 1000).toLocaleString()}。实际可用性以调用结果为准。`
    : '已保存授权；上游未提供到期时间，下次请求将尝试刷新凭据。';
}
function action(id, fn) {
  el(id).addEventListener('click', async () => {
    const button = el(id); button.disabled = true; el('message').textContent = '';
    try { await fn(); } catch (error) { el('message').textContent = error instanceof Error ? error.message : '操作失败'; }
    finally { button.disabled = false; }
  });
}
async function begin(identity_ref) {
  if (session) await request('auth.action', { action: 'cancel', session_id: session });
  const result = await request('auth.action', { action: 'begin', identity_ref });
  session = result.session_id; el('pending').hidden = false; el('message').textContent = '请在上方链接完成 WorkBuddy 官方授权。';
}
action('login', () => begin());
el('json-import').hidden = bridge.get('builtin') !== '1';
action('import', async () => {
  const file = el('import-file').files[0];
  if (file && file.size > 256 * 1024) throw new Error('JSON 文件超过 256 KB');
  const raw = file ? await file.text() : el('import-json').value;
  if (!raw.trim() || new TextEncoder().encode(raw).length > 256 * 1024) throw new Error('请选择或粘贴有效 JSON，最大 256 KB');
  el('import-file').value = '';
  el('import-json').value = '';
  const result = await request('workbuddy.import', { import_json: raw });
  await status();
  const accepted = result.results.filter(row => row.identity_ref);
  if (accepted.length) {
    el('identity').value = accepted[0].identity_ref;
    renderModels();
  }
  const reasons = {
    invalid_credentials_or_region: '凭据或国内区校验失败',
    expiry_or_refresh_required: '缺少有效期或刷新令牌',
    refresh_failed: '刷新令牌失败',
    profile_unavailable: '无法读取账号资料',
    identity_unavailable: '无法确认账号身份',
    catalog_unavailable: '无法读取 WorkBuddy 模型目录',
  };
  const errors = result.results.filter(row => !row.identity_ref)
    .map(row => `第 ${row.index} 项：${reasons[row.reason] ?? '验证失败'}`);
  el('message').textContent = [`已保存 ${accepted.length} 项授权。首次导入的账号可在下方「创建并绑定」。`, ...errors].join('\n');
});
action('reauthorize', () => begin(chosen()));
action('poll', async () => {
  const result = await request('auth.action', { action: 'poll', session_id: session });
  switch (result.reason) {
    case 'session_expired':
      session = '';
      el('pending').hidden = true;
      el('message').textContent = '授权会话已过期，请重新添加账号并使用新链接。';
      return;
    case 'upstream_pending':
      el('message').textContent = 'WorkBuddy 尚未确认本次授权（11217）。请确认当前登录页显示「登录成功」并提示返回 WorkBuddy，然后再次检查。';
      return;
    case 'upstream_refused':
      el('message').textContent = `WorkBuddy 拒绝令牌查询（业务码 ${result.upstream_code}）。`;
      return;
    case 'upstream_unavailable':
      el('message').textContent = 'WorkBuddy 令牌接口暂时不可用或返回异常，请稍后再次检查。';
      return;
    case 'missing_token':
      el('message').textContent = 'WorkBuddy 返回成功，但未签发访问令牌。';
      return;
  }
  if (!result.authorized) throw new Error('未收到有效的授权结果');
  session = ''; el('pending').hidden = true; await status(); el('identity').value = result.identity_ref; renderModels(); el('message').textContent = '授权成功，可以创建或绑定宿主账号。';
});
action('cancel', async () => { await request('auth.action', { action: 'cancel', session_id: session }); session = ''; el('pending').hidden = true; });
action('unlink', async () => { await request('auth.action', { action: 'unlink', identity_ref: chosen() }); await status(); el('message').textContent = '已解除授权，对应宿主账号将无法继续转发。'; });
action('refresh', status);
action('test', async () => { const result = await request('config.test'); el('message').textContent = result.message; });
action('create', async () => { const result = await request('account.create', { identity_ref: chosen(), account_name: el('name').value }); el('account').value = result.account_id; el('message').textContent = `已绑定宿主账号 ${result.account_id}。请到账号管理配置分组后再测试。`; });
action('bind', async () => { const account_id = Number(el('account').value); if (!Number.isSafeInteger(account_id) || account_id < 1) throw new Error('请输入有效账号 ID'); await request('account.bind', { account_id, identity_ref: chosen() }); el('message').textContent = '绑定成功'; });
action('unbind', async () => { const account_id = Number(el('account').value); if (!Number.isSafeInteger(account_id) || account_id < 1) throw new Error('请输入有效账号 ID'); await request('account.unbind', { account_id }); el('message').textContent = '已解除宿主绑定，历史记录保留。需要清除登录材料时，请再解除授权。'; });
el('identity').addEventListener('change', renderModels);
parent.postMessage({ source: 'sub2api-plugin-ui', bridge_token: token, type: 'sub2api.plugin.ready' }, '*');
status().catch(error => { el('message').textContent = error.message; });
