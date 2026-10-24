import http from 'node:http'
import net from 'node:net'

// Runner-local fixture. Only the existing isolated model server is reachable.
const allowed = new Set(['console-upstream:8080', 'chatgpt.com:443'])
const authorization = 'Basic ' + Buffer.from('ci-relay:ci-only').toString('base64')
const sockets = new Set()
let mode = 'healthy'
let attempts = 0
const server = http.createServer((req, res) => {
  if (req.url === '/stats') {
    res.setHeader('Content-Type', 'application/json')
    return res.end(JSON.stringify({ mode, attempts }))
  }
  if (req.method === 'POST' && req.url.startsWith('/mode?')) {
    const next = new URL(req.url, 'http://fixture').searchParams.get('value')
    if (!['healthy', 'reject'].includes(next)) { res.writeHead(400); return res.end() }
    mode = next
    for (const socket of sockets) if (socket !== req.socket) socket.destroy()
    return res.end('ok')
  }
  attempts++
  if (mode === 'reject' || req.headers['proxy-authorization'] !== authorization) {
    res.writeHead(407)
    return res.end()
  }
  let target
  try { target = new URL(req.url) } catch { res.writeHead(400); return res.end() }
  if (target.protocol !== 'http:' || !allowed.has(target.host)) { res.writeHead(403); return res.end() }
  const headers = { ...req.headers }
  delete headers['proxy-authorization']
  delete headers['proxy-connection']
  const upstream = http.request(target, { method: req.method, headers }, response => {
    res.writeHead(response.statusCode, response.headers)
    response.pipe(res)
  })
  upstream.on('error', () => { if (!res.headersSent) res.writeHead(502); res.end() })
  req.pipe(upstream)
})
server.on('connection', socket => {
  sockets.add(socket)
  socket.on('close', () => sockets.delete(socket))
})
server.on('connect', (req, socket, head) => {
  attempts++
  if (mode === 'reject' || req.headers['proxy-authorization'] !== authorization) {
    socket.end('HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n')
    return
  }
  if (!allowed.has(req.url)) { socket.end('HTTP/1.1 403 Forbidden\r\n\r\n'); return }
  const target = new URL('http://' + req.url)
  const upstream = net.connect(Number(target.port), target.hostname, () => {
    socket.write('HTTP/1.1 200 Connection Established\r\n\r\n')
    if (head.length) upstream.write(head)
    socket.pipe(upstream)
    upstream.pipe(socket)
  })
  upstream.on('error', () => socket.destroy())
  socket.on('close', () => upstream.destroy())
})
server.listen(38480, '0.0.0.0', () => console.log('Isolated relay fixture ready'))
