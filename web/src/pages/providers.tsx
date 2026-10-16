import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { BadgeCheck, Briefcase, CalendarDays, ClipboardCheck, Clock, Compass, FileText, Globe2, GraduationCap, Languages, MapPin, Search, SlidersHorizontal } from 'lucide-react'
import { Container } from '../components/Layout'
import { CampusPicker } from '../components/pickers'
import { ProviderCard, ReportButton, VerificationBanner, providerPath, useProviderTags } from '../components/domain'
import { Avatar, Badge, Button, ButtonLink, Empty, ErrorNote, Field, Input, PageLoader, Pagination, Select, Stars, Tabs, Textarea, cx } from '../components/ui'
import { ResumeRequestPanel } from './careers'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { gql } from '../lib/graphql'
import { useAction, useDebounced, useDocumentTitle, useQuery } from '../lib/hooks'
import { CAMPUS, PROVIDER_CARD, SUBJECT } from '../lib/fragments'
import { browserTimeZone, plural, dayKey, formatDate, formatDay, formatMoney, formatTime, minutesToClock, WEEKDAYS, zoneLabel } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import type { Campus, Provider, ProviderKind, SessionMode, Subject } from '../lib/types'

const KIND_COPY: Record<ProviderKind, { title: string; subtitle: string; empty: string }> = {
  TUTOR: { title: 'Find a tutor', subtitle: 'Verified students who know your course, your professors and your exams.', empty: 'No tutors match yet' },
  GUIDE: { title: 'Campus guides', subtitle: 'Tours, hostels, admissions and the city — from students who live there.', empty: 'No guides match yet' },
  MENTOR: { title: 'Placement prep', subtitle: 'Mock interviews and resume reviews from seniors who cracked placements and internships.', empty: 'No mentors match yet' },
}

const KIND_NOUN: Record<ProviderKind, string> = { TUTOR: 'tutor', GUIDE: 'guide', MENTOR: 'mentor' }
const KIND_LIST: Record<ProviderKind, string> = { TUTOR: '/tutors', GUIDE: '/guides', MENTOR: '/mentors' }
const editPath = (kind: ProviderKind) => `/dashboard/profile${kind === 'TUTOR' ? '' : `?kind=${kind}`}`

function MentorIntro() {
  return (
    <div className="mb-6 grid gap-4 sm:grid-cols-2">
      <div className="card flex gap-3 p-5">
        <ClipboardCheck className="h-6 w-6 shrink-0 text-brand-500" />
        <div>
          <h2 className="font-semibold">Mock interviews</h2>
          <p className="mt-1 text-sm text-ink-600">A realistic coding, design, case or HR round. Afterwards you get a scorecard: a score per skill, a verdict, and what to practise next.</p>
        </div>
      </div>
      <div className="card flex gap-3 p-5">
        <FileText className="h-6 w-6 shrink-0 text-brand-500" />
        <div>
          <h2 className="font-semibold">Resume reviews</h2>
          <p className="mt-1 text-sm text-ink-600">Upload your resume for a role. Within the promised days you get a score out of 10 and specific notes on every section.</p>
        </div>
      </div>
    </div>
  )
}

export function Providers({ kind }: { kind: ProviderKind }) {
  const { guideServices, sessionModes, interviewTypes } = useCatalog()
  const copy = KIND_COPY[kind]
  useDocumentTitle(copy.title)
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const [text, setText] = useState(params.get('q') ?? '')
  const [showFilters, setShowFilters] = useState(false)
  const q = useDebounced(text, 350)
  // Mock interviews are online, so mentors from every campus are shown unless one is picked.
  const campusId = params.has('campus') ? params.get('campus') || null : kind === 'MENTOR' ? null : (user?.campus?.id ?? null)
  const page = Number(params.get('page') ?? 1)
  const update = (k: string, v: string | null) => {
    const p = new URLSearchParams(params)
    if (v === null || v === '') p.delete(k)
    else p.set(k, v)
    if (k !== 'page') p.delete('page')
    setParams(p, { replace: true })
  }
  useEffect(() => {
    if ((params.get('q') ?? '') !== q) update('q', q)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q])

  const { data: campusData } = useQuery<{ campus: Campus | null }>(`query($id: ID) { campus(id: $id) { ${CAMPUS} } }`, { id: campusId }, { skip: !campusId })
  const { data: subjectData } = useQuery<{ subjects: Subject[] }>(`query($c: ID) { subjects(campusId: $c) { ${SUBJECT} } }`, { c: campusId }, { skip: kind !== 'TUTOR' })
  const filter = {
    kind,
    campusId,
    query: params.get('q') || null,
    subjectId: params.get('subject') || null,
    mode: params.get('mode') || null,
    service: kind === 'GUIDE' ? params.get('service') || null : null,
    interviewType: kind === 'MENTOR' ? params.get('type') || null : null,
    resumeReviews: kind === 'MENTOR' && params.get('resume') ? true : null,
    minRating: params.get('rating') ? Number(params.get('rating')) : null,
    sort: params.get('sort') || 'RATING',
  }
  const { data, loading, error } = useQuery<{ providers: { total: number; pageSize: number; items: Provider[] } }>(
    `query($f: ProviderFilter!, $p: Int) { providers(filter: $f, page: $p, pageSize: 12) { total pageSize items { ${PROVIDER_CARD} } } }`,
    { f: filter, p: page },
  )

  const filters = (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      {kind === 'TUTOR' ? (
        <Select aria-label="Subject" value={params.get('subject') ?? ''} onChange={(e) => update('subject', e.target.value)}>
          <option value="">All subjects</option>
          {subjectData?.subjects.map((s) => (
            <option key={s.id} value={s.id}>{s.code ? `${s.code} · ` : ''}{s.name}</option>
          ))}
        </Select>
      ) : kind === 'GUIDE' ? (
        <Select aria-label="Service" value={params.get('service') ?? ''} onChange={(e) => update('service', e.target.value)}>
          <option value="">All services</option>
          {guideServices.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </Select>
      ) : (
        <Select aria-label="Offering" value={params.get('resume') ? 'RESUME' : (params.get('type') ?? '')}
          onChange={(e) => {
            const p = new URLSearchParams(params)
            p.delete('page')
            p.delete('type')
            p.delete('resume')
            if (e.target.value === 'RESUME') p.set('resume', '1')
            else if (e.target.value) p.set('type', e.target.value)
            setParams(p, { replace: true })
          }}>
          <option value="">Interviews & resume reviews</option>
          <option value="RESUME">Resume reviews</option>
          {interviewTypes.map((o) => <option key={o.value} value={o.value}>{o.label} interview</option>)}
        </Select>
      )}
      <Select aria-label="Session type" value={params.get('mode') ?? ''} onChange={(e) => update('mode', e.target.value)}>
        <option value="">{sessionModes.map((o) => o.label).join(' or ') || 'Any format'}</option>
        {sessionModes.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
      </Select>
      <Select aria-label="Minimum rating" value={params.get('rating') ?? ''} onChange={(e) => update('rating', e.target.value)}>
        <option value="">Any rating</option>
        <option value="4.5">4.5 ★ and up</option>
        <option value="4">4 ★ and up</option>
        <option value="3">3 ★ and up</option>
      </Select>
      <Select aria-label="Sort" value={filter.sort} onChange={(e) => update('sort', e.target.value)}>
        <option value="RATING">Top rated</option>
        <option value="PRICE_ASC">Price: low to high</option>
        <option value="PRICE_DESC">Price: high to low</option>
        <option value="NEWEST">Newest</option>
      </Select>
    </div>
  )

  return (
    <Container>
      <div className="mb-6">
        <h1 className="text-2xl font-bold sm:text-3xl">{copy.title}</h1>
        <p className="mt-1 text-ink-600">{copy.subtitle}</p>
      </div>
      {kind === 'MENTOR' && <MentorIntro />}
      <div className="card mb-6 space-y-3 p-4">
        <div className="flex flex-col gap-3 sm:flex-row">
          <div className="sm:w-72">
            <CampusPicker value={campusData?.campus ?? null} allowAll onChange={(c) => update('campus', c ? c.id : '')} />
          </div>
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-400" />
            <Input value={text} onChange={(e) => setText(e.target.value)} className="pl-9" placeholder={kind === 'TUTOR' ? 'Search subject, course code or name' : kind === 'MENTOR' ? 'Search company, role or name' : 'Search guides'} aria-label="Search" />
          </div>
          <Button variant="secondary" className="sm:hidden" onClick={() => setShowFilters((s) => !s)}>
            <SlidersHorizontal className="h-4 w-4" /> Filters
          </Button>
        </div>
        <div className={cx(!showFilters && 'hidden sm:block')}>{filters}</div>
      </div>
      {!campusId && kind !== 'MENTOR' && <p className="mb-4 text-sm text-ink-600">Showing every campus. Pick yours to see in-person options nearby.</p>}

      <ErrorNote error={error} />
      {loading && !data ? (
        <PageLoader />
      ) : data && data.providers.items.length === 0 ? (
        <Empty icon={kind === 'TUTOR' ? <GraduationCap className="h-10 w-10" /> : kind === 'MENTOR' ? <Briefcase className="h-10 w-10" /> : <Compass className="h-10 w-10" />} title={copy.empty}
          action={<ButtonLink to={user ? editPath(kind) : `/signup?intent=${kind === 'GUIDE' ? 'guide' : 'teach'}`}>Be the first: become a {KIND_NOUN[kind]}</ButtonLink>}>
          Try another subject, widen the filters, or search all campuses for online sessions.
        </Empty>
      ) : (
        <>
          <p className="mb-3 text-sm text-ink-500">{plural(data?.providers.total ?? 0, KIND_NOUN[kind])}</p>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {data?.providers.items.map((p) => <ProviderCard key={p.id} p={p} />)}
          </div>
          <Pagination page={page} pageSize={12} total={data?.providers.total ?? 0} onChange={(p) => update('page', String(p))} />
        </>
      )}
    </Container>
  )
}

const PROVIDER_DETAIL = `${PROVIDER_CARD} bio
  user { id displayName campusVerified idVerified joinedAt }
  availability { timezone windows { weekday startMinute endMinute } }
  reviews(pageSize: 20) { total items { id rating comment createdAt hidden reviewer { id displayName avatarUrl } } }`

export function ProviderDetail({ kind }: { kind: ProviderKind }) {
  const tagsOf = useProviderTags()
  const { id = '' } = useParams()
  const { data, loading, error } = useQuery<{ provider: Provider | null }>(`query($id: ID!) { provider(id: $id) { ${PROVIDER_DETAIL} } }`, { id })
  const p = data?.provider
  useDocumentTitle(p ? `${p.displayName} — ${{ TUTOR: 'Tutor', GUIDE: 'Guide', MENTOR: 'Career mentor' }[kind]}` : '')
  if (loading) return <PageLoader />
  if (error) return <Container><ErrorNote error={error} /></Container>
  if (!p) return <Container><Empty title="This profile isn't available">It may have been paused or removed.</Empty></Container>

  const tz = p.availability?.timezone ?? 'UTC'
  return (
    <Container>
      <Link to={KIND_LIST[kind]} className="text-sm link">← Back to {KIND_NOUN[kind]}s</Link>
      <div className="mt-4 grid gap-6 lg:grid-cols-[1fr_380px]">
        <div className="space-y-6">
          <section className="card p-6">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
              <Avatar name={p.displayName} url={p.avatarUrl} size={88} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h1 className="text-2xl font-bold">{p.displayName}</h1>
                  {p.user?.idVerified && <Badge tone="brand"><BadgeCheck className="h-3.5 w-3.5" /> ID verified</Badge>}
                  {p.status !== 'ACTIVE' && <Badge tone="yellow">{p.status.toLowerCase()}</Badge>}
                </div>
                <p className="mt-1 text-ink-700">{p.headline}</p>
                <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-sm text-ink-600">
                  {p.campus && <span className="flex items-center gap-1"><MapPin className="h-4 w-4" /> {p.campus.name}</span>}
                  <span className="flex items-center gap-1"><Languages className="h-4 w-4" /> {p.languages.join(', ')}</span>
                  {p.ratingCount > 0 && <span className="flex items-center gap-1"><Stars value={p.ratingAvg} /> {p.ratingAvg.toFixed(1)} ({p.ratingCount})</span>}
                  {p.sessionsCompleted > 0 && <span>{plural(p.sessionsCompleted, kind === 'MENTOR' ? 'interview' : 'session')}</span>}
                  {(p.resumeReviewsDelivered ?? 0) > 0 && <span>{plural(p.resumeReviewsDelivered!, 'resume')} reviewed</span>}
                </div>
              </div>
            </div>
            <div className="mt-6 whitespace-pre-line text-ink-700">{p.bio}</div>
            {p.companies.length > 0 && (
              <div className="mt-6">
                <h2 className="text-sm font-semibold text-ink-900">Worked, interned or placed at</h2>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {p.companies.map((c) => <span key={c} className="chip"><Briefcase className="h-3.5 w-3.5" /> {c}</span>)}
                </div>
              </div>
            )}
            <div className="mt-6">
              <h2 className="text-sm font-semibold text-ink-900">{{ TUTOR: 'Subjects', GUIDE: 'Can help with', MENTOR: 'Offers' }[kind]}</h2>
              <div className="mt-2 flex flex-wrap gap-1.5">
                {(kind === 'TUTOR' ? p.subjects.map((s) => (s.code ? `${s.code} · ${s.name}` : s.name)) : tagsOf(p)).map((t) => (
                  <span key={t} className="chip">{t}</span>
                ))}
              </div>
            </div>
            <div className="mt-6 flex items-center justify-between border-t border-ink-100 pt-4 text-xs text-ink-500">
              <span>Member since {p.user ? formatDate(p.user.joinedAt) : ''}</span>
              {!p.isMine && <ReportButton targetType="PROVIDER" targetId={p.id} label="Report profile" />}
            </div>
          </section>

          <section className="card p-6">
            <h2 className="flex items-center gap-2 font-semibold"><CalendarDays className="h-5 w-5 text-brand-500" /> Weekly availability</h2>
            <p className="mt-1 text-xs text-ink-500">Times in {tz} ({zoneLabel(tz)}).</p>
            {p.availability?.windows.length ? (
              <dl className="mt-3 grid gap-x-6 gap-y-1.5 text-sm sm:grid-cols-2">
                {WEEKDAYS.map((d, i) => {
                  const ws = p.availability!.windows.filter((w) => w.weekday === i)
                  return (
                    <div key={d} className="flex justify-between gap-4 border-b border-ink-100 py-1.5">
                      <dt className="text-ink-600">{d}</dt>
                      <dd className="text-right font-medium">{ws.length ? ws.map((w) => `${minutesToClock(w.startMinute)}–${minutesToClock(w.endMinute)}`).join(', ') : <span className="text-ink-400">—</span>}</dd>
                    </div>
                  )
                })}
              </dl>
            ) : (
              <p className="mt-3 text-sm text-ink-500">No availability set yet.</p>
            )}
          </section>

          <section className="card p-6">
            <h2 className="font-semibold">Reviews {p.reviews?.total ? `(${p.reviews.total})` : ''}</h2>
            {p.reviews?.items.length ? (
              <ul className="mt-4 divide-y divide-ink-100">
                {p.reviews.items.map((r) => (
                  <li key={r.id} className="py-4">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <Avatar name={r.reviewer.displayName} url={r.reviewer.avatarUrl} size={28} />
                        <span className="text-sm font-medium">{r.reviewer.displayName}</span>
                        <Stars value={r.rating} size={12} />
                        {r.hidden && <Badge tone="red">hidden</Badge>}
                      </div>
                      <span className="text-xs text-ink-400">{formatDate(r.createdAt)}</span>
                    </div>
                    {r.comment && <p className="mt-2 text-sm text-ink-700">{r.comment}</p>}
                    <div className="mt-1"><ReportButton targetType="REVIEW" targetId={r.id} /></div>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-sm text-ink-500">No reviews yet. Reviews come from students after completed {kind === 'MENTOR' ? 'interviews and resume reviews' : 'sessions'}.</p>
            )}
          </section>
        </div>
        <aside className="lg:sticky lg:top-20 lg:self-start">
          {kind === 'MENTOR' && !p.isMine ? <MentorPanel p={p} /> : <BookingPanel p={p} />}
        </aside>
      </div>
    </Container>
  )
}

/** A mentor's offers side by side: book a mock interview or send a resume. */
function MentorPanel({ p }: { p: Provider }) {
  const interviews = p.interviewTypes.length > 0
  const [tab, setTab] = useState<'INTERVIEW' | 'RESUME'>(interviews ? 'INTERVIEW' : 'RESUME')
  if (!interviews) return <div className="card p-5"><ResumeRequestPanel p={p} /></div>
  if (!p.resumeOffer) return <BookingPanel p={p} />
  return (
    <div className="card p-5">
      <Tabs value={tab} onChange={setTab} tabs={[{ value: 'INTERVIEW', label: 'Mock interview' }, { value: 'RESUME', label: 'Resume review' }]} />
      <div className="mt-4">{tab === 'INTERVIEW' ? <BookingPanel p={p} bare /> : <ResumeRequestPanel p={p} />}</div>
    </div>
  )
}

function BookingPanel({ p, bare }: { p: Provider; bare?: boolean }) {
  const { label, sessionDurations } = useCatalog()
  const { user } = useAuth()
  const navigate = useNavigate()
  const toast = useToast()
  const [duration, setDuration] = useState(60)
  useEffect(() => {
    if (sessionDurations.length && !sessionDurations.includes(duration)) setDuration(sessionDurations.includes(60) ? 60 : sessionDurations[0])
  }, [sessionDurations, duration])
  const [mode, setMode] = useState<SessionMode>(p.modes[0])
  const [subjectId, setSubjectId] = useState(p.subjects[0]?.id ?? '')
  const [service, setService] = useState(p.services[0] ?? '')
  const [interviewType, setInterviewType] = useState(p.interviewTypes[0] ?? '')
  const [slot, setSlot] = useState<string | null>(null)
  const [day, setDay] = useState<string | null>(null)
  const [notes, setNotes] = useState('')
  const [location, setLocation] = useState('')
  const { data, loading } = useQuery<{ openSlots: { timezone: string; slots: { startAt: string; endAt: string }[] } }>(
    `query($p: ID!, $d: Int!) { openSlots(providerId: $p, durationMinutes: $d, days: 21) { timezone slots { startAt endAt } } }`,
    { p: p.id, d: duration },
  )
  const tz = data?.openSlots.timezone ?? 'UTC'
  const localTz = browserTimeZone()
  const byDay = useMemo(() => {
    const m = new Map<string, string[]>()
    for (const s of data?.openSlots.slots ?? []) {
      const k = dayKey(s.startAt, tz)
      m.set(k, [...(m.get(k) ?? []), s.startAt])
    }
    return [...m.entries()]
  }, [data, tz])
  useEffect(() => {
    setSlot(null)
    setDay((d) => (d && byDay.some(([k]) => k === d) ? d : (byDay[0]?.[0] ?? null)))
  }, [byDay])

  const price = { amountMinor: Math.round((p.rate.amountMinor * duration) / 60), currency: p.rate.currency }
  const book = useAction(async () => {
    const d = await gql<{ requestBooking: { id: string } }>(`mutation($i: BookingInput!) { requestBooking(input: $i) { id } }`, {
      i: {
        providerId: p.id,
        startAt: slot,
        durationMinutes: duration,
        mode,
        subjectId: p.kind === 'TUTOR' ? subjectId : null,
        service: p.kind === 'GUIDE' ? service : null,
        interviewType: p.kind === 'MENTOR' ? interviewType : null,
        notes,
        location: mode === 'IN_PERSON' ? location : '',
      },
    })
    toast('Request sent! You’ll be notified when they reply.')
    navigate(`/bookings/${d.requestBooking.id}`)
  })

  if (p.isMine) {
    return (
      <div className="card p-6">
        <h2 className="font-semibold">This is your profile</h2>
        <p className="mt-1 text-sm text-ink-600">This is how students see you.</p>
        <ButtonLink to={editPath(p.kind)} className="mt-4 w-full" variant="secondary">Edit profile</ButtonLink>
      </div>
    )
  }

  const slotsForDay = byDay.find(([k]) => k === day)?.[1] ?? []
  return (
    <div className={cx(!bare && 'card p-5')}>
      <div className="flex items-baseline justify-between">
        <p className="text-2xl font-bold">{formatMoney(p.rate)}</p>
        {p.rate.amountMinor > 0 && <span className="text-sm text-ink-500">per hour</span>}
      </div>
      <div className="mt-4 space-y-4">
        <div className="grid grid-cols-2 gap-3">
          <Field label="Length">
            <Select value={duration} onChange={(e) => setDuration(Number(e.target.value))}>
              {(sessionDurations.length ? sessionDurations : [duration]).map((m) => <option key={m} value={m}>{m < 60 ? `${m} min` : `${m / 60} hour${m > 60 ? 's' : ''}`}</option>)}
            </Select>
          </Field>
          <Field label="Where">
            <Select value={mode} onChange={(e) => setMode(e.target.value as SessionMode)}>
              {p.modes.map((m) => <option key={m} value={m}>{label('sessionModes', m)}</option>)}
            </Select>
          </Field>
        </div>
        {p.kind === 'TUTOR' ? (
          <Field label="Subject">
            <Select value={subjectId} onChange={(e) => setSubjectId(e.target.value)}>
              {p.subjects.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </Select>
          </Field>
        ) : p.kind === 'MENTOR' ? (
          <Field label="Interview type">
            <Select value={interviewType} onChange={(e) => setInterviewType(e.target.value)}>
              {p.interviewTypes.map((t) => <option key={t} value={t}>{label('interviewTypes', t)}</option>)}
            </Select>
          </Field>
        ) : (
          <Field label="Help with">
            <Select value={service} onChange={(e) => setService(e.target.value)}>
              {p.services.map((s) => <option key={s} value={s}>{label('guideServices', s)}</option>)}
            </Select>
          </Field>
        )}
        <div>
          <p className="label flex items-center gap-1"><Clock className="h-4 w-4" /> Pick a time {data && <span className="font-normal text-ink-500">({zoneLabel(tz)})</span>}</p>
          {loading && !data ? (
            <div className="py-6 text-center text-sm text-ink-500">Loading times…</div>
          ) : byDay.length === 0 ? (
            <p className="rounded-lg bg-ink-50 p-3 text-sm text-ink-600">No open times in the next three weeks. Check back later.</p>
          ) : (
            <>
              <div className="-mx-1 flex gap-1.5 overflow-x-auto px-1 pb-2">
                {byDay.map(([k, list]) => (
                  <button key={k} type="button" onClick={() => { setDay(k); setSlot(null) }}
                    className={cx('shrink-0 rounded-lg border px-3 py-1.5 text-xs font-medium', day === k ? 'border-brand-500 bg-brand-50 text-brand-800' : 'border-ink-200 text-ink-700 hover:border-ink-300')}>
                    {formatDay(list[0], tz)}
                  </button>
                ))}
              </div>
              <div className="grid grid-cols-3 gap-1.5">
                {slotsForDay.map((s) => (
                  <button key={s} type="button" onClick={() => setSlot(s)}
                    className={cx('rounded-lg border py-1.5 text-sm font-medium', slot === s ? 'border-brand-500 bg-brand-500 text-white' : 'border-ink-200 text-ink-800 hover:border-brand-300')}>
                    {formatTime(s, tz)}
                  </button>
                ))}
              </div>
              {slot && formatTime(slot, localTz) !== formatTime(slot, tz) && <p className="mt-2 text-xs text-ink-500">That's {formatTime(slot, localTz)} your time ({zoneLabel(localTz)}).</p>}
            </>
          )}
        </div>
        {mode === 'IN_PERSON' && (
          <Field label="Suggested place" hint="They can confirm or suggest another spot.">
            <Input value={location} onChange={(e) => setLocation(e.target.value)} placeholder="e.g. Central library, 2nd floor" maxLength={200} />
          </Field>
        )}
        <Field label="Message (optional)">
          <Textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={3} maxLength={1000} placeholder={p.kind === 'MENTOR' ? 'Which company or role are you preparing for?' : 'What do you need help with?'} />
        </Field>
        {user && <VerificationBanner user={user} need="campus" />}
        <ErrorNote error={book.error} />
        <div className="flex items-center justify-between border-t border-ink-100 pt-3 text-sm">
          <span className="text-ink-600">Total</span>
          <span className="font-bold">{formatMoney(price)}</span>
        </div>
        {user ? (
          <Button className="w-full" size="lg" disabled={!slot || !user.campusVerified} loading={book.pending} onClick={() => book.run()}>{p.kind === 'MENTOR' ? 'Request mock interview' : 'Request session'}</Button>
        ) : (
          <ButtonLink to={`/login?next=${encodeURIComponent(providerPath(p))}`} className="w-full" size="lg">Log in to book</ButtonLink>
        )}
        <p className="flex items-start gap-1.5 text-xs text-ink-500">
          <Globe2 className="mt-0.5 h-3.5 w-3.5 shrink-0" /> You pay {p.displayName.split(' ')[0]} directly, as you agree. Nothing is charged by Your Mate.
        </p>
      </div>
    </div>
  )
}
