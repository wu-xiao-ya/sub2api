import http from 'node:http'
import { randomUUID } from 'node:crypto'
import { setTimeout as delay } from 'node:timers/promises'
import { pathToFileURL } from 'node:url'

// Runner-local protocol fixture. No forwarding and no real upstream credentials.
export const server = http.createServer(async (req, res) => {
  if (req.method === 'GET' && req.url === '/health') {
    res.end('ok')
    return
  }
  const path = new URL(req.url, 'http://fixture.invalid').pathname
  const protocol = path.endsWith('/responses') ? 'responses' : path.endsWith('/chat/completions') ? 'chat'
    : path.endsWith('/messages') ? 'anthropic' : /(?:\/models\/[^/]+|\/v1internal):(streamGenerateContent|generateContent)$/.test(path) ? 'gemini' : null
  if (req.method !== 'POST' || !protocol) {
    res.writeHead(404).end()
    return
  }
  const oauth = req.headers.authorization === 'Bearer ci-oauth-fixture-only'
  const authorized = protocol === 'anthropic' ? req.headers['x-api-key'] === 'sk-ci-fixture-only' || oauth
    : protocol === 'gemini' ? req.headers['x-goog-api-key'] === 'sk-ci-fixture-only' || oauth
      : req.headers.authorization === 'Bearer sk-ci-fixture-only' || oauth
  if (!authorized) {
    res.writeHead(401, { 'Content-Type': 'application/json' }).end(JSON.stringify({ error: { type: 'authentication_error' } }))
    return
  }
  let raw = ''
  for await (const part of req) raw += part
  const body = JSON.parse(raw)
  const mappedModel = protocol === 'gemini' && path.includes('/models/') ? path.split('/models/')[1].split(':')[0] : body.model
  // Matrix requests must reach the mapped upstream model, not its public alias.
  const standardClaudeOAuth = protocol === 'anthropic' && oauth && mappedModel === 'claude-sonnet-4-5-20250929'
  if (JSON.stringify(body).includes('ci-matrix') && !mappedModel.endsWith('-ci-mapped') && !standardClaudeOAuth) {
    res.writeHead(400, { 'Content-Type': 'application/json' }).end(JSON.stringify({ error: { type: 'mapping_missing' } }))
    return
  }
  if (protocol !== 'responses') {
    await serveNativeProtocol({ req, res, body, protocol, mappedModel, path })
    return
  }
  if (JSON.stringify(body.input).includes('ci-fail')) {
    res.writeHead(503, { 'Content-Type': 'application/json' }).end(JSON.stringify({ error: { type: 'server_error', code: 'upstream_unavailable', message: 'CI fixture unavailable' } }))
    return
  }
  const id = 'resp_ci_' + randomUUID()
  const item = { id: 'msg_' + id, type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text: 'hello', annotations: [] }] }
  const response = { id, object: 'response', status: 'completed', model: body.model, output: [item],
    service_tier: body.service_tier ?? 'default', usage: { input_tokens: 10, output_tokens: 3, total_tokens: 13,
      input_tokens_details: { cached_tokens: 0 }, output_tokens_details: { reasoning_tokens: 0 } } }
  // Account creation checks Responses tool support before choosing a protocol.
  if (body.tool_choice === 'required' && body.tools?.some(tool => tool.name === 'probe_ping')) {
    response.output = [{ id: 'fc_' + id, type: 'function_call', status: 'completed', call_id: 'call_' + id,
      name: 'probe_ping', arguments: JSON.stringify({ ok: true }) }]
    res.writeHead(200, { 'Content-Type': 'application/json', 'x-request-id': id }).end(JSON.stringify(response))
    return
  }
  if (!body.stream) {
    await delay(25)
    res.writeHead(200, { 'Content-Type': 'application/json', 'x-request-id': id }).end(JSON.stringify(response))
    return
  }
  res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', 'x-request-id': id })
  res.flushHeaders()
  const event = (type, fields) => res.write('event: ' + type + '\ndata: ' + JSON.stringify({ type, ...fields }) + '\n\n')
  await delay(20)
  event('response.created', { response: { ...response, status: 'in_progress', output: [], usage: null } })
  await delay(JSON.stringify(body.input).includes('ci-hidden') ? 250 : 25)
  event('response.output_item.added', { output_index: 0, item: { ...item, status: 'in_progress', content: [] } })
  event('response.content_part.added', { item_id: item.id, output_index: 0, content_index: 0, part: { type: 'output_text', text: '', annotations: [] } })
  event('response.output_text.delta', { item_id: item.id, output_index: 0, content_index: 0, delta: 'hello' })
  await delay(40)
  event('response.output_text.done', { item_id: item.id, output_index: 0, content_index: 0, text: 'hello' })
  event('response.content_part.done', { item_id: item.id, output_index: 0, content_index: 0, part: item.content[0] })
  event('response.output_item.done', { output_index: 0, item })
  event('response.completed', { response })
  res.end()
})

async function serveNativeProtocol({ res, body, protocol, mappedModel, path }) {
  const id = 'ci_' + randomUUID()
  const stream = protocol === 'gemini' ? path.endsWith(':streamGenerateContent') : body.stream
  const chat = { id, object: 'chat.completion', model: mappedModel, choices: [{ index: 0, message: { role: 'assistant', content: 'hello' }, finish_reason: 'stop' }],
    usage: { prompt_tokens: 10, completion_tokens: 3, total_tokens: 13 } }
  const anthropic = { id, type: 'message', role: 'assistant', model: mappedModel, content: [{ type: 'text', text: 'hello' }],
    stop_reason: 'end_turn', stop_sequence: null, usage: { input_tokens: 10, output_tokens: 3 } }
  const gemini = { candidates: [{ index: 0, content: { role: 'model', parts: [{ text: 'hello' }] }, finishReason: 'STOP' }],
    usageMetadata: { promptTokenCount: 10, candidatesTokenCount: 3, totalTokenCount: 13 }, modelVersion: mappedModel, responseId: id }
  const wrap = data => path.startsWith('/v1internal:') ? { response: data } : data
  if (!stream) {
    await delay(25)
    res.writeHead(200, { 'Content-Type': 'application/json', 'x-request-id': id }).end(JSON.stringify(wrap({ chat, anthropic, gemini }[protocol])))
    return
  }
  res.writeHead(200, { 'Content-Type': 'text/event-stream', 'x-request-id': id })
  res.flushHeaders()
  const event = (data, name) => res.write((name ? 'event: ' + name + '\n' : '') + 'data: ' + JSON.stringify(data) + '\n\n')
  const claude = (type, fields) => event({ type, ...fields }, type)
  const chunk = (delta, finish_reason = null) => ({ id, object: 'chat.completion.chunk', model: mappedModel,
    choices: [{ index: 0, delta, finish_reason }] })
  await delay(20)
  if (protocol === 'anthropic') {
    claude('message_start', { message: { ...anthropic, content: [], stop_reason: null, usage: { input_tokens: 10, output_tokens: 0 } } })
    claude('ping', {})
    claude('content_block_start', { index: 0, content_block: { type: 'text', text: '' } })
  } else if (protocol === 'chat') event(chunk({ role: 'assistant', content: '' }))
  else res.write(': heartbeat\n\n')
  await delay(25)
  if (protocol === 'anthropic') claude('content_block_delta', { index: 0, delta: { type: 'text_delta', text: 'hello' } })
  else if (protocol === 'chat') event(chunk({ content: 'hello' }))
  else event(wrap({ ...gemini, candidates: [{ index: 0, content: { role: 'model', parts: [{ text: 'hello' }] } }], usageMetadata: undefined }))
  await delay(40)
  if (protocol === 'anthropic') {
    claude('content_block_stop', { index: 0 })
    claude('message_delta', { delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 3 } })
    claude('message_stop', {})
  } else if (protocol === 'chat') {
    event({ ...chunk({}, 'stop'), usage: chat.usage })
    res.write('data: [DONE]\n\n')
  } else event(wrap({ ...gemini, candidates: [{ index: 0, content: { role: 'model', parts: [] }, finishReason: 'STOP' }] }))
  res.end()
}
if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) server.listen(8080, '0.0.0.0')
