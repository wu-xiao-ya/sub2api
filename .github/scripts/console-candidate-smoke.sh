#!/usr/bin/env bash
set -euo pipefail
out=/tmp/console-candidate-smoke
mkdir -p "$out"
# This script is intentionally runner-local and has no production endpoints.
cleanup() {
  docker logs console-app > "$out/app.log" 2>&1 || true
  docker logs console-db > "$out/database.log" 2>&1 || true
  docker logs console-web > "$out/nginx.log" 2>&1 || true
  docker rm -f console-web console-app console-db console-redis >/dev/null 2>&1 || true
  docker network rm console-ci >/dev/null 2>&1 || true
}
trap cleanup EXIT
docker network create --internal console-ci
docker run -d --name console-db --network console-ci --network-alias postgres   -e POSTGRES_PASSWORD=isolated-ci-only -e POSTGRES_USER=sub2api -e POSTGRES_DB=sub2api postgres:18-alpine
docker run -d --name console-redis --network console-ci --network-alias redis redis:8-alpine
for i in $(seq 1 60); do
  if docker exec console-db pg_isready -U sub2api -d sub2api; then break; fi
  sleep 2
done
docker exec console-db pg_isready -U sub2api -d sub2api
docker run -d --name console-app --network console-ci --memory=1536m --cpus=2   -e AUTO_SETUP=true -e DATABASE_HOST=postgres -e DATABASE_PORT=5432   -e DATABASE_USER=sub2api -e DATABASE_PASSWORD=isolated-ci-only -e DATABASE_DBNAME=sub2api -e DATABASE_SSLMODE=disable   -e REDIS_HOST=redis -e REDIS_PORT=6379 -e REDIS_POOL_SIZE=20   -e ADMIN_EMAIL=ci@example.invalid -e ADMIN_PASSWORD=Isolated-CI-Password-39   -e JWT_SECRET=isolated-ci-only-secret-no-production-access   sub2api-console-candidate:local
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
docker run -d --name console-web --network console-ci -p 127.0.0.1:18091:80   -v "$out/nginx.conf:/etc/nginx/nginx.conf:ro" nginx:alpine
for i in $(seq 1 90); do
  if curl -fsS http://127.0.0.1:18091/health > "$out/health.json"; then break; fi
  sleep 2
done
curl -fsS http://127.0.0.1:18091/health > "$out/health.json"
curl -fsS http://127.0.0.1:18091/starlightai/health > "$out/subpath-health.json"
curl -fsS http://127.0.0.1:18091/api/v1/settings/public > "$out/settings.json"
node --input-type=module <<'NODE'
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
const base = 'http://127.0.0.1:18091'
const checks = []
for (const path of ['/', '/starlightai/', '/login', '/starlightai/login']) {
  const response = await fetch(base + path)
  assert(response.ok, path + ' must return 200')
  assert(response.headers.get('content-type')?.includes('text/html'), path + ' must return HTML')
  const html = await response.text()
  assert(html.includes('id="app"'), 'Missing Vue mount point')
  const assets = [...html.matchAll(/(?:src|href)="([^"<>]+\.(?:js|css))"/g)].map(match => match[1])
  assert(assets.some(asset => asset.endsWith('.js')) && assets.some(asset => asset.endsWith('.css')), 'Missing embedded build assets')
  for (const asset of assets) {
    const url = new URL(asset, base + path)
    assert.equal(url.origin, base, 'Asset must be served locally')
    const resource = await fetch(url)
    assert(resource.ok, 'Missing ' + asset)
    assert(!resource.headers.get('content-type')?.includes('text/html'), 'Asset returned SPA HTML: ' + asset)
    assert((await resource.arrayBuffer()).byteLength > 100, 'Empty asset')
  }
  checks.push({ path, assets, status: response.status })
}
await writeFile('/tmp/console-candidate-smoke/pages.json', JSON.stringify(checks, null, 2))
console.log(JSON.stringify(checks, null, 2))
NODE
