// Live message delivery over a WebSocket (graphql-transport-ws). One socket
// is shared by every component that listens. The gateway ends a subscription
// when the access token behind it expires, so we reconnect with a fresh token
// whenever it completes. Polling stays on as a slow fallback.

import { useEffect, useRef } from 'react'
import { createClient, type Client } from 'graphql-ws'
import { getAccessToken, refreshSession } from './graphql'

export interface MessageEvent {
  conversationId: string
  message: { id: string; body: string; createdAt: string; mine: boolean }
}

type Listener = (e: MessageEvent) => void

const listeners = new Set<Listener>()
let client: Client | null = null
let unsubscribe: (() => void) | null = null
let retry: ReturnType<typeof setTimeout> | undefined
let failures = 0

/** Seconds until the JWT expires (0 when unreadable). */
function secondsLeft(token: string): number {
  try {
    const { exp } = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')))
    return exp - Date.now() / 1000
  } catch {
    return 0
  }
}

async function freshToken(): Promise<string | null> {
  let token = getAccessToken()
  if (!token || secondsLeft(token) < 30) {
    if (!(await refreshSession())) return null
    token = getAccessToken()
  }
  return token
}

function socketURL(): string {
  const { protocol, host } = window.location
  return `${protocol === 'https:' ? 'wss' : 'ws'}://${host}/graphql`
}

function start() {
  if (unsubscribe || listeners.size === 0) return
  client ??= createClient({
    url: socketURL(),
    lazy: true,
    retryAttempts: 0, // reconnects are ours, so each one gets a fresh token
    connectionParams: async () => {
      const token = await freshToken()
      if (!token) throw new Error('logged out')
      return { Authorization: `Bearer ${token}` }
    },
  })
  unsubscribe = client.subscribe<{ messageEvents: MessageEvent }>(
    { query: `subscription { messageEvents { conversationId message { id body createdAt mine } } }` },
    {
      next: ({ data }) => {
        failures = 0
        if (data) listeners.forEach((l) => l(data.messageEvents))
      },
      // The token ran out: open a new socket with a new one.
      complete: () => restart(0),
      error: () => restart(Math.min(30_000, 1000 * 2 ** failures++)),
    },
  )
}

function restart(delay: number) {
  teardown()
  if (listeners.size === 0 || !getAccessToken()) return
  retry = setTimeout(start, delay)
}

function teardown() {
  clearTimeout(retry)
  unsubscribe?.()
  unsubscribe = null
  client?.dispose()
  client = null
}

/** Closes the socket, e.g. on logout. */
export function stopRealtime() {
  teardown()
  failures = 0
}

/** Calls cb for every message sent or received by the viewer, live. */
export function useMessageEvents(cb: Listener, enabled = true) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    if (!enabled) return
    const l: Listener = (e) => ref.current(e)
    listeners.add(l)
    start()
    return () => {
      listeners.delete(l)
      if (listeners.size === 0) teardown()
    }
  }, [enabled])
}
