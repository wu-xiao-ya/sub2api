import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'
import { execFileSync } from 'node:child_process'

export async function verifyTrafficRelay({ api, admin, base }) {
  const auth = { token: admin.access_token }
  const post = { ...auth, method: 'POST' }
  const saved = await api('/api/v1/admin/settings', auth)
  const proxy = async (name, host) => api('/api/v1/admin/proxies', { ...post, body: {
    name, protocol: 'http', host, port: 38480, username: 'ci-relay', password: 'ci-only'
  } })
  const relay = await proxy('CI relay only', 'console-relay')
  const own = await proxy('CI account default', 'console-default-proxy')
  const group = await api('/api/v1/admin/groups', { ...post, body: {
    name: 'CI relay isolation', platform: 'openai', rate_multiplier: 1, is_exclusive: true
  } })
  const account = await api('/api/v1/admin/accounts', { ...post, body: {
    name: 'CI relay account', platform: 'openai', type: 'apikey', concurrency: 2, priority: 1,
    credentials: { api_key: 'sk-ci-fixture-only', base_url: 'http://console-upstream:8080', model_mapping: { 'gpt-5.1': 'gpt-5.1' } },
    group_ids: [group.id], proxy_id: own.id, use_relay_route: true, upstream_billing_probe_enabled: false
  } })
  await api('/api/v1/admin/users', { ...post, body: {
    email: 'relay-user@example.invalid', password: 'Isolated-Relay-Password-39',
    role: 'user', concurrency: 2, balance: 10, allowed_groups: [group.id]
  } })
  const user = await api('/api/v1/auth/login', { method: 'POST', body: {
    email: 'relay-user@example.invalid', password: 'Isolated-Relay-Password-39'
  } })
  const key = await api('/api/v1/keys', { token: user.access_token, method: 'POST', body: {
    name: 'CI relay key', group_id: group.id
  } })
  const control = (container, path, method = 'GET') => execFileSync('docker', [
    'exec', container, 'node', '--input-type=module', '-e',
    `const r=await fetch(${JSON.stringify('http://127.0.0.1:38480' + path)}, {method:${JSON.stringify(method)}}); if(!r.ok) process.exit(1); console.log(await r.text())`
  ], { encoding: 'utf8', timeout: 15000 }).trim()
  const stats = async port => JSON.parse(control(port === 18093 ? 'console-relay' : 'console-default-proxy', '/stats'))
  const mode = async value => {
    assert.equal(control('console-relay', '/mode?value=' + value, 'POST'), 'ok')
  }
  const request = async () => {
    const response = await fetch(base + '/v1/responses', {
      method: 'POST', headers: { Authorization: 'Bearer ' + key.key, 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: 'gpt-5.1', input: 'ci-relay-isolation', stream: true, max_output_tokens: 16 }),
      signal: AbortSignal.timeout(30000)
    })
    assert.equal(response.status, 200, 'Relay candidate request failed')
    const body = await response.text()
    assert(body.includes('response.completed') && body.includes('hello'))
  }
  const evidence = []
  try {
    await api('/api/v1/admin/settings', { ...auth, method: 'PUT', body: {
      ...saved, traffic_relay_enabled: true, traffic_relay_proxy_id: relay.id,
      traffic_relay_unavailable_ttl_seconds: 5
    } })
    await delay(5500)
    // Account creation runs capability probes before relay settings take effect.
    // Compare only traffic caused by the request under test.
    const beforeRelay = await stats(18093)
    const beforeDefault = await stats(18094)
    await request()
    const initial = await stats(18093)
    assert(initial.attempts > beforeRelay.attempts, 'Opted-in account did not use relay')
    assert.equal((await stats(18094)).attempts, beforeDefault.attempts, 'Healthy relay used fallback')
    evidence.push('healthy_relay')
    await mode('reject')
    await request()
    const failed = await stats(18093)
    const fallback = await stats(18094)
    assert(failed.attempts > initial.attempts)
    assert(fallback.attempts > beforeDefault.attempts, 'Relay failure did not use account proxy')
    evidence.push('same_request_fallback')
    await request()
    assert.equal((await stats(18093)).attempts, failed.attempts, 'Cooldown retried relay')
    assert((await stats(18094)).attempts > fallback.attempts)
    evidence.push('shared_cooldown')
    const active = await api('/api/v1/admin/accounts/' + account.id, auth)
    assert.equal(active.schedulable, true, 'Relay fault unscheduled account')
    assert.equal(active.status, 'active')
    await mode('healthy')
    await delay(5500)
    await request()
    const recovered = await stats(18093)
    assert(recovered.attempts > failed.attempts, 'Relay failed to recover after TTL')
    evidence.push('ttl_recovery')
    await api('/api/v1/admin/accounts/' + account.id, { ...auth, method: 'PUT', body: { use_relay_route: false } })
    await delay(1000)
    await request()
    assert.equal((await stats(18093)).attempts, recovered.attempts, 'Unchecked account used relay')
    evidence.push('unchecked_default_route')
    await writeFile('/tmp/console-candidate-smoke/traffic-relay.json', JSON.stringify({ checks: evidence }, null, 2))
  } finally {
    await api('/api/v1/admin/settings', { ...auth, method: 'PUT', body: saved })
  }
}
