import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'

// Only disposable accounts and credentials; the app/upstream network has no internet route.
export async function verifyPlatformMatrix({ api, admin, base }) {
  assert.equal(base, 'http://127.0.0.1:18091')
  const post = { token: admin.access_token, method: 'POST' }
  const specs = [
    ['anthropic', 'claude-sonnet-4-5', 'messages'], ['gemini', 'gemini-2.5-flash', 'gemini'],
    ['grok', 'grok-4', 'responses'], ['deepseek', 'deepseek-chat', 'chat'],
    ['kimi', 'kimi-k2', 'chat'], ['glm', 'glm-4.5', 'chat'], ['qwen', 'qwen-plus', 'chat'],
    ['minimax', 'MiniMax-M2', 'chat'], ['mimo', 'mimo-v2-flash', 'chat'], ['hunyuan', 'hunyuan-turbos-latest', 'chat'],
    ['anthropic', 'claude-sonnet-4-5', 'messages', 'oauth'],
    ['gemini', 'gemini-2.5-flash', 'gemini', 'oauth'], ['grok', 'grok-4', 'responses', 'oauth']
  ]
  const cases = []
  for (const [platform, model, protocol, type = 'apikey'] of specs) {
    const name = 'CI matrix ' + platform + ' ' + type
    const group = await api('/api/v1/admin/groups', { ...post, body: { name, platform, rate_multiplier: 1, is_exclusive: true } })
    const credentials = { base_url: 'http://console-upstream:8080', model_mapping: { [model]: model + '-ci-mapped' },
      ...(type === 'apikey' ? { api_key: 'sk-ci-fixture-only' } : {
        access_token: 'ci-oauth-fixture-only', refresh_token: 'ci-refresh-fixture-only',
        expires_at: new Date(Date.now() + 86400000).toISOString()
      }) }
    await api('/api/v1/admin/accounts', { ...post, body: { name, platform, type, credentials,
      extra: type === 'oauth' && platform === 'anthropic' ? { custom_base_url_enabled: true, custom_base_url: credentials.base_url } : {},
      concurrency: 2, priority: 1, group_ids: [group.id], upstream_billing_probe_enabled: false } })
    await api('/api/v1/admin/channels', { ...post, body: { name, group_ids: [group.id], model_pricing: [{
      platform, models: [model, model + '-ci-mapped'], billing_mode: 'token', input_price: 0.000001, output_price: 0.000002
    }] } })
    cases.push({ platform, model, protocol, type, name, groupId: group.id })
  }
  await api('/api/v1/admin/users', { ...post, body: { email: 'matrix-user@example.invalid', password: 'Isolated-Matrix-Password-39',
    role: 'user', concurrency: 2, balance: 10, allowed_groups: cases.map(c => c.groupId) } })
  const user = await api('/api/v1/auth/login', { method: 'POST', body: { email: 'matrix-user@example.invalid', password: 'Isolated-Matrix-Password-39' } })
  for (const c of cases) {
    const key = await api('/api/v1/keys', { token: user.access_token, method: 'POST', body: { name: c.name, group_id: c.groupId } })
    for (const stream of [true, false]) {
      const path = c.protocol === 'gemini' ? '/v1beta/models/' + c.model + ':' + (stream ? 'streamGenerateContent?alt=sse' : 'generateContent')
        : '/v1/' + (c.protocol === 'chat' ? 'chat/completions' : c.protocol)
      const body = c.protocol === 'gemini' ? { contents: [{ role: 'user', parts: [{ text: 'ci-matrix' }] }], generationConfig: { maxOutputTokens: 16 } }
        : { model: c.model, stream, ...(c.protocol === 'responses' ? { input: 'ci-matrix', max_output_tokens: 16 }
          : { messages: [{ role: 'user', content: 'ci-matrix' }], max_tokens: 16 }) }
      const response = await fetch(base + path, { method: 'POST', headers: { Authorization: 'Bearer ' + key.key,
        'Content-Type': 'application/json', 'anthropic-version': '2023-06-01' }, body: JSON.stringify(body), signal: AbortSignal.timeout(45000) })
      const text = await response.text()
      assert.equal(response.status, 200, c.name + ' stream=' + stream + ' gateway status=' + response.status)
      assert(text.includes('hello'), c.name + ' missing actual text')
      console.log('Platform gateway passed: ' + c.name + ' stream=' + stream)
    }
  }
  let usage
  const usageDeadline = Date.now() + 30000
  do {
    usage = await api('/api/v1/usage?page_size=100', { token: user.access_token })
    if (usage.items.length === cases.length * 2) break
    await delay(1000)
  } while (Date.now() < usageDeadline)
  assert.equal(usage.items.length, cases.length * 2, 'Each successful request must create exactly one usage row')
  for (const c of cases) {
    const rows = usage.items.filter(row => row.group_id === c.groupId)
    assert.equal(rows.length, 2, c.name)
    for (const row of rows) {
      const label = c.name + ' stream=' + row.stream
      assert(row.actual_cost > 0 && row.actual_cost < 0.001, label + ' must bill mapped token prices')
      const stages = row.latency_breakdown
      assert.equal(stages?.version, 2, label + ' ingress milestones')
      for (const field of ['first_response_ms', 'first_output_ms', 'first_character_ms', 'total_duration_ms']) {
        assert(Number.isFinite(stages[field]) && stages[field] >= 0 && stages[field] <= stages.total_duration_ms, label + ' ' + field)
      }
      if (row.stream) assert(Number.isFinite(stages.first_event_ms), label + ' valid event')
      else assert(stages.first_event_ms == null, label + ' must not invent stream events')
      assert.equal(row.requested_model || row.model, c.model, label + ' public model attribution')
    }
  }
  const deadline = Date.now() + 180000
  let metrics
  do {
    metrics = await api('/api/v1/channels/performance?range=90m', { token: user.access_token })
    if (cases.every(c => metrics.items.some(m => m.key === c.name + '\0' + c.platform + '\0' + c.model && m.success_rate === 100 && m.output_tps > 0))) break
    await delay(5000)
  } while (Date.now() < deadline)
  for (const c of cases) {
    const m = metrics.items.find(m => m.key === c.name + '\0' + c.platform + '\0' + c.model)
    assert(m?.success_rate === 100 && m.output_tps > 0 && m.first_character_ms >= 0, c.name + ' aggregated performance')
  }
  await writeFile('/tmp/console-candidate-smoke/performance-platform-matrix.json', JSON.stringify({ cases, metrics, usage: usage.items.map(row => ({
    group_id: row.group_id, model: row.model, requested_model: row.requested_model, stream: row.stream, actual_cost: row.actual_cost, latency_breakdown: row.latency_breakdown
  })) }, null, 2))
  console.log('Routed platform matrix passed: ' + cases.length + ' account paths, ' + usage.items.length + ' requests')
}
