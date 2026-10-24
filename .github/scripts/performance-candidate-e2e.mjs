import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'

export async function verifyPerformanceRequests({ api, admin, user, userId, base }) {
  const adminOptions = { token: admin.access_token, method: 'POST' }
  const model = 'gpt-5.1'
  const visible = await api('/api/v1/admin/groups', { ...adminOptions, body: { name: 'CI visible', platform: 'openai', rate_multiplier: 1, is_exclusive: true } })
  const hidden = await api('/api/v1/admin/groups', { ...adminOptions, body: { name: 'CI hidden', platform: 'openai', rate_multiplier: 1, is_exclusive: true } })
  for (const group of [visible, hidden]) {
    assert(Number.isSafeInteger(group.id))
    await api('/api/v1/admin/accounts', { ...adminOptions, body: {
      name: 'CI account ' + group.id, platform: 'openai', type: 'apikey', concurrency: 2, priority: 1,
      credentials: { api_key: 'sk-ci-fixture-only', base_url: 'http://console-upstream:8080', model_mapping: { [model]: model } },
      group_ids: [group.id], upstream_billing_probe_enabled: false
    } })
  }
  const channelName = 'CI performance'
  await api('/api/v1/admin/channels', { ...adminOptions, body: {
    name: channelName, group_ids: [visible.id, hidden.id], model_pricing: [{ platform: 'openai', models: [model], billing_mode: 'token', input_price: 0.000001, output_price: 0.000002 }]
  } })
  await api('/api/v1/admin/users/' + userId, { token: admin.access_token, method: 'PUT', body: { allowed_groups: [visible.id] } })
  const funded = await api('/api/v1/admin/users/' + userId + '/balance', { ...adminOptions, body: { balance: 10, operation: 'set', notes: 'Isolated performance fixture funding' } })
  assert.equal(funded.balance, 10, 'Fixture user must have spendable balance before gateway requests')
  await api('/api/v1/admin/users', { ...adminOptions, body: {
    email: 'hidden-user@example.invalid', password: 'Isolated-Hidden-Password-39', role: 'user', concurrency: 1, balance: 10, allowed_groups: [hidden.id]
  } })
  const other = await api('/api/v1/auth/login', { method: 'POST', body: { email: 'hidden-user@example.invalid', password: 'Isolated-Hidden-Password-39' } })
  const key = await api('/api/v1/keys', { token: user.access_token, method: 'POST', body: { name: 'CI visible key', group_id: visible.id } })
  const otherKey = await api('/api/v1/keys', { token: other.access_token, method: 'POST', body: { name: 'CI hidden key', group_id: hidden.id } })
  assert(key.key && otherKey.key, 'Expected ephemeral API credentials')
  async function request(apiKey, input, stream, failure = false) {
    const result = await fetch(base + '/v1/responses', { method: 'POST', headers: { Authorization: 'Bearer ' + apiKey, 'Content-Type': 'application/json' },
      body: JSON.stringify({ model, input, stream, service_tier: 'priority', reasoning: { effort: 'high' }, max_output_tokens: 16 }), signal: AbortSignal.timeout(45000) })
    const text = await result.text()
    let errorCode
    try {
      const error = JSON.parse(text)?.error
      errorCode = error?.code ?? error?.type
    } catch {}
    const safeCode = typeof errorCode === 'string' && /^[a-zA-Z0-9_]{1,80}$/.test(errorCode) ? errorCode : 'unavailable'
    if (failure) assert(result.status >= 500, 'Expected provider failure; status=' + result.status + ', code=' + safeCode)
    else {
      assert.equal(result.status, 200, 'Isolated gateway request failed; code=' + safeCode)
      if (stream) assert(text.includes('response.completed') && text.includes('hello'))
      else assert.equal(JSON.parse(text).status, 'completed')
    }
  }
  await request(key.key, 'ci-visible', true)
  await request(key.key, 'ci-visible', false)
  await request(otherKey.key, 'ci-hidden', true)
  await request(key.key, 'ci-fail', true, true)
  const cardKey = channelName + '\0openai\0' + model
  const path = '/api/v1/channels/performance/detail?' + new URLSearchParams({ key: cardKey, range: '90m' })
  // Requests in the current minute intentionally appear only after its bucket closes.
  const deadline = Date.now() + 180000
  let metrics, otherMetrics
  while (Date.now() < deadline) {
    metrics = await api(path, { token: user.access_token })
    otherMetrics = await api('/starlightai' + path, { token: other.access_token })
    const a = metrics.items[0], b = otherMetrics.items[0]
    if (a?.output_tps > 0 && Math.abs(a.success_rate - 200 / 3) < 0.001 && b?.success_rate === 100) break
    await delay(5000)
  }
  await writeFile('/tmp/console-candidate-smoke/performance-observed.json', JSON.stringify({ visible: metrics, hidden: otherMetrics }, null, 2))
  const visibleMetric = metrics.items[0], hiddenMetric = otherMetrics.items[0]
  assert(Math.abs(visibleMetric.success_rate - 200 / 3) < 0.001, 'Two successes plus one failed logical request must count once each')
  assert.equal(hiddenMetric.success_rate, 100)
  assert(visibleMetric.first_character_ms >= 0 && visibleMetric.duration_ms > 0 && visibleMetric.output_tps > 0)
  assert.deepEqual(visibleMetric.groups.map(g => g.id), [visible.id])
  assert.deepEqual(hiddenMetric.groups.map(g => g.id), [hidden.id])
  assert.equal(metrics.source, 'user_requests')
  assert.equal(visibleMetric.sample_quality, 'low')
  await api(path + '&group_id=' + hidden.id, { token: user.access_token, status: 404 })
  const nonStream = await api(path + '&stream=false', { token: user.access_token })
  assert.equal(nonStream.items[0].success_rate, 100)
  assert.equal(nonStream.items[0].output_tps, null, 'Non-streaming output must not contribute TPS')
  const unknown = await api(path + '&service_tier=unknown', { token: user.access_token })
  assert.equal(unknown.items[0].success_rate, null, 'Known request tiers must not be relabeled as unknown')
  const encoded = JSON.stringify(metrics)
  for (const privateValue of ['CI hidden', 'CI account', 'api_key', 'user_id', key.key, otherKey.key]) assert(!encoded.includes(privateValue), 'Performance response exposed private data')
  const usage = await api('/api/v1/usage?page_size=20', { token: user.access_token })
  assert.equal(usage.items.length, 2, 'Only the two successful billable requests should have usage records')
  for (const row of usage.items) {
    assert.equal(row.group_id, visible.id)
    assert(row.actual_cost > 0 && row.actual_cost < 0.001, 'Fixture token prices must not exhaust the funded balance')
    const stages = row.latency_breakdown
    assert.equal(stages?.version, 2, 'Real usage must retain ingress-based milestones')
    for (const field of ['first_response_ms', 'first_output_ms', 'first_character_ms', 'total_duration_ms']) {
      assert(Number.isFinite(stages[field]) && stages[field] >= 0, 'Missing usage milestone: ' + field)
      assert(stages[field] <= stages.total_duration_ms)
    }
    if (row.stream) assert(Number.isFinite(stages.first_event_ms))
    else assert(stages.first_event_ms == null, 'Non-streaming usage must not invent a protocol event')
  }
  await writeFile('/tmp/console-candidate-smoke/performance-requests.json', JSON.stringify({ visible: visibleMetric, hidden: hiddenMetric, non_stream: nonStream.items[0] }, null, 2))
  console.log('Isolated real-request performance aggregation and cross-user permissions passed')
}
