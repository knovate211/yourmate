import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Check, ChevronsUpDown, MapPin, Search, X } from 'lucide-react'
import { useDebounced, useQuery } from '../lib/hooks'
import { CAMPUS, SUBJECT } from '../lib/fragments'
import type { Campus, Subject } from '../lib/types'
import { cx } from './ui'

/** Searchable campus combobox over every active campus worldwide. */
export function CampusPicker({
  value,
  onChange,
  allowAll,
  placeholder = 'Search your university or college',
  id,
  className,
}: {
  value: Campus | null
  onChange: (c: Campus | null) => void
  allowAll?: boolean
  placeholder?: string
  id?: string
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const query = useDebounced(text, 200)
  const ref = useRef<HTMLDivElement>(null)
  const { data, loading } = useQuery<{ campuses: Campus[] }>(
    `query($q: String) { campuses(query: $q, limit: 30) { ${CAMPUS} } }`,
    { q: query },
    { skip: !open },
  )
  useEffect(() => {
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [])

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        id={id}
        onClick={() => setOpen((o) => !o)}
        className={cx('input flex items-center justify-between gap-2 text-left', className)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className={cx('flex min-w-0 items-center gap-2', !value && 'text-ink-400')}>
          <MapPin className="h-4 w-4 shrink-0 text-ink-400" />
          <span className="truncate">{value ? value.name : allowAll ? 'All campuses' : placeholder}</span>
        </span>
        <ChevronsUpDown className="h-4 w-4 shrink-0 text-ink-400" />
      </button>
      {open && (
        <div className="absolute z-40 mt-1 w-full min-w-72 overflow-hidden rounded-xl border border-ink-200 bg-white shadow-lg">
          <div className="flex items-center gap-2 border-b border-ink-100 px-3">
            <Search className="h-4 w-4 text-ink-400" />
            <input autoFocus value={text} onChange={(e) => setText(e.target.value)} placeholder="Name, city, country or email domain" className="w-full py-2.5 text-sm outline-none" />
          </div>
          <ul role="listbox" className="max-h-72 overflow-y-auto py-1">
            {allowAll && (
              <li>
                <button type="button" className="flex w-full items-center justify-between px-3 py-2 text-left text-sm hover:bg-ink-50" onClick={() => { onChange(null); setOpen(false) }}>
                  All campuses (online sessions worldwide)
                  {!value && <Check className="h-4 w-4 text-brand-500" />}
                </button>
              </li>
            )}
            {data?.campuses.map((c) => (
              <li key={c.id}>
                <button
                  type="button"
                  role="option"
                  aria-selected={value?.id === c.id}
                  className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left hover:bg-ink-50"
                  onClick={() => {
                    onChange(c)
                    setOpen(false)
                  }}
                >
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-medium text-ink-900">{c.name}</span>
                    <span className="block truncate text-xs text-ink-500">
                      {[c.city, c.country].filter(Boolean).join(', ')}
                      {c.emailDomains.length > 0 && ` · @${c.emailDomains[0]}`}
                    </span>
                  </span>
                  {value?.id === c.id && <Check className="h-4 w-4 shrink-0 text-brand-500" />}
                </button>
              </li>
            ))}
            {!loading && data?.campuses.length === 0 && <li className="px-3 py-3 text-sm text-ink-500">No campus matches “{text}”.</li>}
          </ul>
          <div className="border-t border-ink-100 px-3 py-2 text-xs text-ink-600">
            Can't find yours?{' '}
            <Link to="/campuses/request" className="link">
              Add your campus
            </Link>
          </div>
        </div>
      )}
    </div>
  )
}

/** Multi-select of subjects (global + the campus's own), grouped by category. */
export function SubjectPicker({ campusId, value, onChange, max = 15 }: { campusId?: string; value: string[]; onChange: (ids: string[]) => void; max?: number }) {
  const [filter, setFilter] = useState('')
  const { data } = useQuery<{ subjects: Subject[] }>(`query($c: ID) { subjects(campusId: $c) { ${SUBJECT} } }`, { c: campusId ?? null })
  const groups = useMemo(() => {
    const f = filter.trim().toLowerCase()
    const m = new Map<string, Subject[]>()
    for (const s of data?.subjects ?? []) {
      if (f && !s.name.toLowerCase().includes(f) && !s.code.toLowerCase().includes(f)) continue
      const g = s.campusId ? 'Your campus' : s.category
      m.set(g, [...(m.get(g) ?? []), s])
    }
    return [...m.entries()]
  }, [data, filter])
  const byId = useMemo(() => new Map((data?.subjects ?? []).map((s) => [s.id, s])), [data])
  const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : value.length >= max ? value : [...value, id])

  return (
    <div className="rounded-xl border border-ink-200">
      {value.length > 0 && (
        <div className="flex flex-wrap gap-1.5 border-b border-ink-100 p-2.5">
          {value.map((id) => (
            <button type="button" key={id} onClick={() => toggle(id)} className="inline-flex items-center gap-1 rounded-full bg-brand-100 px-2.5 py-1 text-xs font-medium text-brand-800 hover:bg-brand-200">
              {byId.get(id)?.name ?? '…'} <X className="h-3 w-3" />
            </button>
          ))}
        </div>
      )}
      <div className="flex items-center gap-2 border-b border-ink-100 px-3">
        <Search className="h-4 w-4 text-ink-400" />
        <input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter subjects" className="w-full py-2 text-sm outline-none" />
      </div>
      <div className="max-h-64 overflow-y-auto p-2">
        {groups.map(([group, subs]) => (
          <div key={group} className="mb-2">
            <p className="px-1 py-1 text-[11px] font-semibold uppercase tracking-wide text-ink-500">{group}</p>
            <div className="flex flex-wrap gap-1.5">
              {subs.map((s) => (
                <button
                  type="button"
                  key={s.id}
                  onClick={() => toggle(s.id)}
                  className={cx(
                    'rounded-full border px-2.5 py-1 text-xs font-medium transition',
                    value.includes(s.id) ? 'border-brand-500 bg-brand-500 text-white' : 'border-ink-200 bg-white text-ink-700 hover:border-ink-300',
                  )}
                >
                  {s.code ? `${s.code} · ` : ''}
                  {s.name}
                </button>
              ))}
            </div>
          </div>
        ))}
        {groups.length === 0 && <p className="px-1 py-2 text-sm text-ink-500">No subjects match.</p>}
      </div>
    </div>
  )
}
