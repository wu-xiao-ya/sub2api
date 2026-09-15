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
  if (req.method !== 'POST' || !req.url.endsWith('/responses')) {
    res.writeHead(404).end()
    return
  }
  if (req.headers.authorization !== 'Bearer sk-ci-fixture-only') {
    res.writeHead(401, { 'Content-Type': 'application/json' }).end(JSON.stringify({ error: { type: 'authentication_error' } }))
    return
  }
  let raw = ''
  for await (const part of req) raw += part
  const body = JSON.parse(raw)
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
if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) server.listen(8080, '0.0.0.0')
