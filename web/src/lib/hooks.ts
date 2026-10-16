import { useCallback, useEffect, useRef, useState } from 'react'
import { gql } from './graphql'

interface QueryState<T> {
  data: T | undefined
  error: unknown
  loading: boolean
  refetch: () => Promise<void>
  setData: (fn: (d: T | undefined) => T | undefined) => void
}

/**
 * Runs a query when its variables change. With pollMs, re-runs quietly in
 * the background while the tab is visible.
 */
export function useQuery<T>(
  query: string,
  variables?: Record<string, unknown>,
  opts: { skip?: boolean; pollMs?: number } = {},
): QueryState<T> {
  const [data, setDataState] = useState<T>()
  const [error, setError] = useState<unknown>()
  const [loading, setLoading] = useState(!opts.skip)
  const key = JSON.stringify(variables ?? {})
  const seq = useRef(0)

  const run = useCallback(
    async (quiet = false) => {
      const mine = ++seq.current
      if (!quiet) setLoading(true)
      try {
        const d = await gql<T>(query, variables)
        if (mine === seq.current) {
          setDataState(d)
          setError(undefined)
        }
      } catch (e) {
        if (mine === seq.current && !quiet) setError(e)
      } finally {
        if (mine === seq.current && !quiet) setLoading(false)
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [query, key],
  )

  useEffect(() => {
    if (opts.skip) {
      setLoading(false)
      return
    }
    run()
  }, [run, opts.skip])

  useEffect(() => {
    if (!opts.pollMs || opts.skip) return
    const id = setInterval(() => {
      if (document.visibilityState === 'visible') run(true)
    }, opts.pollMs)
    return () => clearInterval(id)
  }, [run, opts.pollMs, opts.skip])

  return {
    data,
    error,
    loading,
    refetch: () => run(true),
    setData: (fn) => setDataState((d) => fn(d)),
  }
}

/** Wraps an async action with pending/error state. */
export function useAction<A extends unknown[], R>(fn: (...args: A) => Promise<R>) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const run = useCallback(
    async (...args: A): Promise<R | undefined> => {
      setPending(true)
      setError(undefined)
      try {
        return await fn(...args)
      } catch (e) {
        setError(e)
        return undefined
      } finally {
        setPending(false)
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [fn],
  )
  return { run, pending, error, setError }
}

export function useDebounced<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return v
}

export function useDocumentTitle(title: string) {
  useEffect(() => {
    document.title = title ? `${title} · Your Mate` : 'Your Mate — learn or earn on your campus'
  }, [title])
}
