import assert from 'node:assert/strict'
import { once } from 'node:events'
import test from 'node:test'
import { server } from './performance-upstream.mjs'

test('isolated upstream supports capability probe, text, streaming and failure', async () => {
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const url = 'http://127.0.0.1:' + server.address().port + '/v1/responses'
  const request = body => fetch(url, { method: 'POST', headers: {
    Authorization: 'Bearer sk-ci-fixture-only', 'Content-Type': 'application/json'
  }, body: JSON.stringify({ model: 'gpt-5.1', input: 'hello', ...body }) })
  try {
    const probe = await request({ tools: [{ type: 'function', name: 'probe_ping' }], tool_choice: 'required', stream: false })
    assert.equal(probe.status, 200)
    const tool = (await probe.json()).output[0]
    assert.equal(tool.type, 'function_call')
    assert.equal(tool.name, 'probe_ping')
    assert.deepEqual(JSON.parse(tool.arguments), { ok: true })
    const plain = await request({ stream: false })
    assert.equal((await plain.json()).output[0].content[0].text, 'hello')
    const streaming = await request({ stream: true })
    const events = await streaming.text()
    assert(events.includes('response.output_text.delta') && events.includes('response.completed'))
    const failure = await request({ input: 'ci-fail', stream: true })
    assert.equal(failure.status, 503)
    await failure.text()
    for (const protocol of ['chat', 'anthropic', 'gemini']) {
      for (const oauth of [false, true]) {
        for (const stream of [false, true]) {
          const path = protocol === 'chat' ? '/v1/chat/completions' : protocol === 'anthropic' ? '/v1/messages?beta=true'
            : '/v1beta/models/gemini-ci-mapped:' + (stream ? 'streamGenerateContent?alt=sse' : 'generateContent')
          const headers = { 'Content-Type': 'application/json' }
          if (oauth) headers.Authorization = 'Bearer ci-oauth-fixture-only'
          else if (protocol === 'anthropic') headers['x-api-key'] = 'sk-ci-fixture-only'
          else if (protocol === 'gemini') headers['x-goog-api-key'] = 'sk-ci-fixture-only'
          else headers.Authorization = 'Bearer sk-ci-fixture-only'
          const body = { model: 'test-ci-mapped', messages: [{ role: 'user', content: 'ci-matrix' }], stream }
          const response = await fetch(new URL(path, url), { method: 'POST', headers, body: JSON.stringify(body) })
          assert.equal(response.status, 200, protocol + ' oauth=' + oauth + ' stream=' + stream)
          const text = await response.text()
          assert(text.includes('hello'))
          if (!stream) assert(JSON.parse(text))
          const unauthorized = await fetch(new URL(path, url), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
          assert.equal(unauthorized.status, 401)
          await unauthorized.text()
        }
      }
    }
    const unmapped = await request({ model: 'public-model', input: 'ci-matrix' })
    assert.equal(unmapped.status, 400)
    await unmapped.text()
  } finally {
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()))
  }
})
