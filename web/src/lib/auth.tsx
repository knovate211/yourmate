import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { bootSession, gql, onSession, refreshSession, setAccessToken } from './graphql'
import { ME } from './fragments'
import type { User } from './types'
import { stopRealtime } from './realtime'

interface AuthPayload {
  accessToken: string
  expiresAt: string
  user: User
}

interface AuthState {
  user: User | null
  /** True until the first session restore attempt finishes. */
  booting: boolean
  login: (email: string, password: string) => Promise<User>
  verifyEmail: (email: string, code: string) => Promise<User>
  logout: () => Promise<void>
  reload: () => Promise<void>
  setUser: (u: User) => void
}

const AuthContext = createContext<AuthState | null>(null)

const AUTH_FIELDS = `accessToken expiresAt user { ${ME} }`

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUserState] = useState<User | null>(null)
  const [booting, setBooting] = useState(true)
  // Start restoring the session during the first render: child components'
  // effects run before this provider's, and their queries must wait for it.
  const [boot] = useState(() => bootSession())
  const timer = useRef<ReturnType<typeof setTimeout>>()

  // Refresh a minute before the access token expires so requests rarely 401.
  const schedule = useCallback((expiresAt: string | null) => {
    clearTimeout(timer.current)
    if (!expiresAt) return
    const ms = new Date(expiresAt).getTime() - Date.now() - 60_000
    timer.current = setTimeout(() => refreshSession(), Math.max(ms, 5_000))
  }, [])

  const loadMe = useCallback(async () => {
    const d = await gql<{ me: User | null }>(`{ me { ${ME} } }`)
    setUserState(d.me)
  }, [])

  useEffect(() => {
    onSession((s) => {
      schedule(s?.expiresAt ?? null)
      if (!s) setUserState(null)
    })
    ;(async () => {
      if (await boot) {
        try {
          await loadMe()
        } catch {
          setUserState(null)
        }
      }
      setBooting(false)
    })()
    return () => clearTimeout(timer.current)
  }, [boot, loadMe, schedule])

  const adopt = useCallback(
    (p: AuthPayload) => {
      setAccessToken(p.accessToken)
      schedule(p.expiresAt)
      setUserState(p.user)
      return p.user
    },
    [schedule],
  )

  const value: AuthState = {
    user,
    booting,
    login: async (email, password) => {
      const d = await gql<{ login: AuthPayload }>(
        `mutation($e: String!, $p: String!) { login(email: $e, password: $p) { ${AUTH_FIELDS} } }`,
        { e: email, p: password },
      )
      return adopt(d.login)
    },
    verifyEmail: async (email, code) => {
      const d = await gql<{ verifyEmail: AuthPayload }>(
        `mutation($e: String!, $c: String!) { verifyEmail(email: $e, code: $c) { ${AUTH_FIELDS} } }`,
        { e: email, c: code },
      )
      return adopt(d.verifyEmail)
    },
    logout: async () => {
      try {
        await gql(`mutation { logout }`)
      } finally {
        setAccessToken(null)
        stopRealtime()
        clearTimeout(timer.current)
        setUserState(null)
      }
    },
    reload: loadMe,
    setUser: setUserState,
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth outside AuthProvider')
  return ctx
}
