import https from 'node:https'
import { readFileSync } from 'node:fs'
import { server } from './performance-upstream.mjs'

// Test CA is scoped to the isolated app container, never the host trust store.
https.createServer({ key: readFileSync('/fixture-tls/server.key'), cert: readFileSync('/fixture-tls/server.crt') },
  server.listeners('request')[0]).listen(443, '0.0.0.0')
server.listen(8080, '0.0.0.0')
