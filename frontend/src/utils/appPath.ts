const KNOWN_APP_BASE_PATHS = ['/starlightai']

function normalizeBasePath(value: string): string {
  const raw = value.trim()
  if (!raw || raw === '/') return '/'
  const withLeadingSlash = raw.startsWith('/') ? raw : `/${raw}`
  return `${withLeadingSlash.replace(/\/+$/, '')}/`
}

export function getAppBasePath(): string {
  const configured = normalizeBasePath(String(import.meta.env.VITE_BASE_PATH || ''))
  if (configured !== '/') return configured

  if (typeof window !== 'undefined') {
    const pathname = window.location.pathname
    for (const knownPath of KNOWN_APP_BASE_PATHS) {
      if (pathname === knownPath || pathname.startsWith(`${knownPath}/`)) {
        return `${knownPath}/`
      }
    }
  }

  return '/'
}

export function buildAppPath(path: string): string {
  const normalized = path ? (path.startsWith('/') ? path : `/${path}`) : '/'
  const base = getAppBasePath()
  if (base === '/') return normalized

  const baseWithoutTrailingSlash = base.slice(0, -1)
  if (normalized === baseWithoutTrailingSlash || normalized.startsWith(base)) {
    return normalized
  }
  return `${baseWithoutTrailingSlash}${normalized}`
}

export function buildAppOriginUrl(path: string): string {
  if (typeof window === 'undefined') return buildAppPath(path)
  return `${window.location.origin}${buildAppPath(path)}`
}
