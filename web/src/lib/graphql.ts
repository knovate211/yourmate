// Minimal GraphQL client. The access token lives only in memory; the refresh
// token is an httpOnly cookie the browser sends to /graphql. When the gateway
// says the access token expired, we refresh once and replay the request.

export class GqlError extends Error {
  code: string
  reason?: string
  constructor(message: string, code = 'INTERNAL', reason?: string) {
    super(message)
    this.code = code
    this.reason = reason
  }
}

interface RawError {
  message: string
  extensions?: { code?: string; reason?: string }
}

let accessToken: string | null = null
let refreshing: Promise<boolean> | null = null
// The first session restore on page load. Requests wait for it, so a page that
// queries on mount never runs as a logged-out visitor by mistake.
let booting: Promise<unknown> | null = null

/** Restores the session from the refresh cookie once, at app start. */
export function bootSession(): Promise<boolean> {
  const p = refreshSession()
  booting = p.catch(() => false)
  return p
}
let onSessionChange: ((s: SessionPayload | null) => void) | null = null

export interface SessionPayload {
  accessToken: string
  expiresAt: string
}

export function setAccessToken(token: string | null) {
  accessToken = token
}

export function getAccessToken(): string | null {
  return accessToken
}

export function onSession(cb: (s: SessionPayload | null) => void) {
  onSessionChange = cb
}

async function post(body: BodyInit, headers: Record<string, string>): Promise<Response> {
  const h: Record<string, string> = { ...headers }
  if (accessToken) h.Authorization = `Bearer ${accessToken}`
  return fetch('/graphql', { method: 'POST', body, headers: h, credentials: 'same-origin' })
}

async function parse<T>(res: Response): Promise<T> {
  let json: { data?: T; errors?: RawError[] }
  try {
    json = await res.json()
  } catch {
    throw new GqlError(res.ok ? 'Unexpected response from the server.' : `The server is unavailable (${res.status}). Try again shortly.`)
  }
  if (json.errors?.length) {
    const e = json.errors[0]
    throw new GqlError(e.message, e.extensions?.code, e.extensions?.reason)
  }
  return json.data as T
}

/** Refreshes the session from the cookie. Concurrent callers share one call. */
export function refreshSession(): Promise<boolean> {
  if (!refreshing) {
    refreshing = (async () => {
      try {
        accessToken = null
        const res = await post(
          JSON.stringify({ query: `mutation { refreshSession { accessToken expiresAt } }` }),
          { 'Content-Type': 'application/json' },
        )
        const data = await parse<{ refreshSession: SessionPayload | null }>(res)
        accessToken = data.refreshSession?.accessToken ?? null
        onSessionChange?.(data.refreshSession)
        return !!accessToken
      } catch {
        accessToken = null
        onSessionChange?.(null)
        return false
      } finally {
        refreshing = null
      }
    })()
  }
  return refreshing
}

async function send<T>(make: () => [BodyInit, Record<string, string>]): Promise<T> {
  if (booting) await booting
  const [body, headers] = make()
  let res = await post(body, headers)
  if (res.status === 401) {
    const ok = await refreshSession()
    if (!ok) throw new GqlError('Your session ended. Log in again.', 'UNAUTHENTICATED')
    const [b2, h2] = make()
    res = await post(b2, h2)
  }
  return parse<T>(res)
}

export function gql<T>(query: string, variables?: Record<string, unknown>): Promise<T> {
  return send<T>(() => [JSON.stringify({ query, variables }), { 'Content-Type': 'application/json' }])
}

/** Sends a mutation with files, following the GraphQL multipart request spec. */
export function gqlUpload<T>(query: string, variables: Record<string, unknown>, files: Record<string, File>): Promise<T> {
  return send<T>(() => {
    const form = new FormData()
    const vars = { ...variables }
    const map: Record<string, string[]> = {}
    Object.keys(files).forEach((path, i) => {
      map[String(i)] = [`variables.${path}`]
      setPath(vars, path, null)
    })
    form.append('operations', JSON.stringify({ query, variables: vars }))
    form.append('map', JSON.stringify(map))
    Object.values(files).forEach((f, i) => form.append(String(i), f))
    // The gateway requires this header on multipart posts as CSRF protection.
    return [form, { 'Apollo-Require-Preflight': 'true' }]
  })
}

function setPath(obj: Record<string, unknown>, path: string, value: unknown) {
  const parts = path.split('.')
  let cur = obj as Record<string, unknown>
  for (let i = 0; i < parts.length - 1; i++) {
    cur[parts[i]] = { ...((cur[parts[i]] as Record<string, unknown>) ?? {}) }
    cur = cur[parts[i]] as Record<string, unknown>
  }
  cur[parts[parts.length - 1]] = value
}

export function errorMessage(e: unknown): string {
  if (e instanceof GqlError) return e.message
  if (e instanceof Error) return e.message
  return 'Something went wrong.'
}
