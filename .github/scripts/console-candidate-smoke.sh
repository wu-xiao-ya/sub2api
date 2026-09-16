#!/usr/bin/env bash
set -euo pipefail
out=/tmp/console-candidate-smoke
mkdir -p "$out"
# This script is intentionally runner-local and has no production endpoints.
cleanup() {
  docker logs console-app > "$out/app.log" 2>&1 || true
  docker logs console-db > "$out/database.log" 2>&1 || true
  docker logs console-web > "$out/nginx.log" 2>&1 || true
  docker logs console-upstream > "$out/upstream.log" 2>&1 || true
  docker rm -f console-web console-app console-db console-redis console-upstream >/dev/null 2>&1 || true
  docker network rm console-ci >/dev/null 2>&1 || true
  docker network rm console-ui >/dev/null 2>&1 || true
}
trap cleanup EXIT
node --test .github/scripts/performance-upstream.test.mjs
docker network create --internal console-ci
docker network create console-ui
tlsdir=$(mktemp -d)
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -keyout "$tlsdir/server.key" -out "$tlsdir/server.crt" \
  -subj '/CN=chatgpt.com' -addext 'subjectAltName=DNS:chatgpt.com,DNS:cloudcode-pa.googleapis.com,DNS:daily-cloudcode-pa.googleapis.com'
# The private fixture key is outside the uploaded evidence directory.
docker run -d --name console-upstream --network console-ci --network-alias console-upstream \
  --network-alias chatgpt.com --network-alias cloudcode-pa.googleapis.com --network-alias daily-cloudcode-pa.googleapis.com \
  -v "$PWD/.github/scripts:/fixture:ro" -v "$tlsdir:/fixture/tls:ro" node:24-alpine node /fixture/performance-upstream-tls.mjs
docker run -d --name console-db --network console-ci --network-alias postgres   -e POSTGRES_PASSWORD=isolated-ci-only -e POSTGRES_USER=sub2api -e POSTGRES_DB=sub2api postgres:18-alpine
docker run -d --name console-redis --network console-ci --network-alias redis redis:8-alpine
for i in $(seq 1 60); do
  # The image initializes using a temporary socket-only server which stops
  # again. TCP becomes available only after initialization has completed.
  if docker exec console-db pg_isready -h 127.0.0.1 -U sub2api -d sub2api; then break; fi
  sleep 2
done
docker exec console-db pg_isready -h 127.0.0.1 -U sub2api -d sub2api
docker run -d --name console-app --network console-ci --memory=1536m --cpus=2 \
  -v "$tlsdir/server.crt:/fixture-ca.crt:ro" -e SSL_CERT_FILE=/fixture-ca.crt \
  -e AUTO_SETUP=true -e DATABASE_HOST=postgres -e DATABASE_PORT=5432   -e DATABASE_USER=sub2api -e DATABASE_PASSWORD=isolated-ci-only -e DATABASE_DBNAME=sub2api -e DATABASE_SSLMODE=disable   -e REDIS_HOST=redis -e REDIS_PORT=6379 -e REDIS_POOL_SIZE=20 -e REDIS_MIN_IDLE_CONNS=2   -e ADMIN_EMAIL=ci@example.invalid -e ADMIN_PASSWORD=Isolated-CI-Password-39   -e JWT_SECRET=isolated-ci-only-secret-no-production-access   sub2api-console-candidate:local
deadline=$((SECONDS + 180))
until docker exec console-app wget -q -T 5 -O /dev/null http://127.0.0.1:8080/health; do
  if [ "$(docker inspect --format '{{.State.Running}}' console-app)" != true ]; then
    echo 'Candidate application exited before becoming ready' >&2
    exit 1
  fi
  if (( SECONDS >= deadline )); then
    echo 'Candidate application readiness deadline exceeded' >&2
    exit 1
  fi
  sleep 2
done
cat > "$out/nginx.conf" <<'NGINX'
events {}
http {
  server {
    listen 80;
    location /starlightai/ { proxy_pass http://console-app:8080/; }
    location / { proxy_pass http://console-app:8080; }
  }
}
NGINX
# Only the proxy has a published host port; backend services remain isolated.
docker create --name console-web --network console-ui -p 127.0.0.1:18091:80   -v "$out/nginx.conf:/etc/nginx/nginx.conf:ro" nginx:alpine
docker network connect console-ci console-web
docker start console-web
deadline=$((SECONDS + 60))
until curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:18091/health > "$out/health.json"; do
  if [ "$(docker inspect --format '{{.State.Running}}' console-web)" != true ] || (( SECONDS >= deadline )); then
    echo 'Candidate proxy failed to become ready' >&2
    exit 1
  fi
  sleep 2
done
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:18091/health > "$out/health.json"
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:18091/starlightai/health > "$out/subpath-health.json"
curl --connect-timeout 2 --max-time 5 -fsS http://127.0.0.1:18091/api/v1/settings/public > "$out/settings.json"
node --input-type=module <<'NODE'
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { verifyPerformanceRequests } from './.github/scripts/performance-candidate-e2e.mjs'
import { verifyPlatformMatrix } from './.github/scripts/performance-platform-matrix.mjs'
const base = 'http://127.0.0.1:18091'
const checks = []
for (const path of ['/', '/starlightai/', '/login', '/starlightai/login']) {
  const response = await fetch(base + path, { signal: AbortSignal.timeout(10000) })
  assert(response.ok, path + ' must return 200')
  assert(response.headers.get('content-type')?.includes('text/html'), path + ' must return HTML')
  const html = await response.text()
  assert(html.includes('id="app"'), 'Missing Vue mount point')
  const assets = [...html.matchAll(/(?:src|href)="([^"<>]+\.(?:js|css))"/g)].map(match => match[1])
  assert(assets.some(asset => asset.endsWith('.js')) && assets.some(asset => asset.endsWith('.css')), 'Missing embedded build assets')
  for (const asset of assets) {
    const url = new URL(asset, base + path)
    assert.equal(url.origin, base, 'Asset must be served locally')
    const resource = await fetch(url, { signal: AbortSignal.timeout(10000) })
    assert(resource.ok, 'Missing ' + asset)
    assert(!resource.headers.get('content-type')?.includes('text/html'), 'Asset returned SPA HTML: ' + asset)
    assert((await resource.arrayBuffer()).byteLength > 100, 'Empty asset')
  }
  checks.push({ path, assets, status: response.status })
}
await writeFile('/tmp/console-candidate-smoke/pages.json', JSON.stringify(checks, null, 2))
console.log(JSON.stringify(checks, null, 2))
const apiChecks = []
async function api(path, { token, method = 'GET', body, status = 200 } = {}) {
  const response = await fetch(base + path, {
    method,
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(10000)
  })
  assert.equal(response.status, status, method + ' ' + path)
  const result = await response.json()
  apiChecks.push({ method, path, status: response.status })
  return result.data
}
// Disposable CI-only identities. Never persist tokens or authentication bodies.
const admin = await api('/api/v1/auth/login', { method: 'POST', body: {
  email: 'ci@example.invalid', password: 'Isolated-CI-Password-39'
} })
assert(admin?.access_token, 'Candidate admin login failed')
// Exercise the first-login gate only for this disposable, runner-local admin.
await api('/api/v1/admin/settings', { token: admin.access_token, status: 423 })
const compliance = await api('/api/v1/admin/compliance', { token: admin.access_token })
assert(compliance?.ack_phrase_en, 'Missing first-login confirmation challenge')
await api('/api/v1/admin/compliance/accept', { token: admin.access_token, method: 'POST', body: {
  phrase: compliance.ack_phrase_en, language: 'en'
} })
const settings = await api('/api/v1/admin/settings', { token: admin.access_token })
await api('/api/v1/admin/settings', { token: admin.access_token, method: 'PUT', body: { ...settings, available_channels_enabled: true } })
const createdUser = await api('/api/v1/admin/users', { token: admin.access_token, method: 'POST', body: {
  email: 'performance-user@example.invalid', password: 'Isolated-User-Password-39', role: 'user', concurrency: 1, balance: 0
} })
const user = await api('/starlightai/api/v1/auth/login', { method: 'POST', body: {
  email: 'performance-user@example.invalid', password: 'Isolated-User-Password-39'
} })
assert(user?.access_token, 'Candidate user login through subpath failed')
for (const prefix of ['', '/starlightai']) {
  await api(prefix + '/api/v1/channels/performance', { status: 401 })
  for (const range of ['90m', '24h', '7d', '30d']) {
    const data = await api(prefix + '/api/v1/channels/performance?range=' + range, { token: user.access_token })
    assert.equal(data.version, 2)
    assert.equal(data.source, 'user_requests')
    assert.deepEqual(data.items, [], 'A user without available channels must receive no other group data')
    assert(Date.parse(data.start) < Date.parse(data.end))
  }
  await api(prefix + '/api/v1/channels/performance?group_id=999999', { token: user.access_token, status: 404 })
  await api(prefix + '/api/v1/channels/performance/detail?key=unavailable', { token: user.access_token, status: 404 })
  await api(prefix + '/api/v1/channels/performance?stream=invalid', { token: user.access_token, status: 400 })
}
await api('/api/v1/admin/users', { token: user.access_token, status: 403 })
await verifyPerformanceRequests({ api, admin, user, userId: createdUser.id, base })
await verifyPlatformMatrix({ api, admin, base })
await writeFile('/tmp/console-candidate-smoke/performance-api.json', JSON.stringify(apiChecks, null, 2))
console.log('Authenticated performance and permission checks passed: ' + apiChecks.length)
NODE
node .github/scripts/performance-candidate-browser.mjs
