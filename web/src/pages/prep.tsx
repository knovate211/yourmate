import { useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { BookOpen, Briefcase, ClipboardCheck, FileText, Plus, Search, Trash2, TrendingUp, Users } from 'lucide-react'
import { Container } from '../components/Layout'
import { ProviderCard, VerificationBanner } from '../components/domain'
import { Avatar, Badge, Button, ButtonLink, Empty, ErrorNote, Field, Input, Modal, PageHeader, PageLoader, Pagination, Select, Tabs, Textarea, cx, type Tone } from '../components/ui'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { gql } from '../lib/graphql'
import { useAction, useDebounced, useDocumentTitle, useQuery } from '../lib/hooks'
import { EXPERIENCE, PROVIDER_CARD, QUESTION } from '../lib/fragments'
import { formatDate, plural, slotLabel } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import type { Difficulty, ExperienceOutcome, InterviewExperience, Progress, Provider, Question } from '../lib/types'

// Placement prep: the hub for mock interviews, resume reviews, seniors'
// interview experiences and a student's own progress.

type TabId = 'interviews' | 'resumes' | 'experiences' | 'progress'
const TABS: { value: TabId; label: string }[] = [
  { value: 'interviews', label: 'Mock interviews' },
  { value: 'resumes', label: 'Resume reviews' },
  { value: 'experiences', label: 'Interview experiences' },
  { value: 'progress', label: 'My progress' },
]

const MENTOR_CARD = `${PROVIDER_CARD} nextSlotAt replyMinutes`

export function PrepHub() {
  useDocumentTitle('Placement prep')
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const tab = (TABS.find((t) => t.value === params.get('tab'))?.value ?? 'interviews') as TabId
  const { data: mine } = useQuery<{ myProviderProfiles: { kind: string; status: string }[] }>(`{ myProviderProfiles { kind status } }`, undefined, { skip: !user })
  const isMentor = mine?.myProviderProfiles.some((p) => p.kind === 'MENTOR' && p.status !== 'SUSPENDED')
  return (
    <Container>
      <div className="mb-5 grid gap-4 lg:grid-cols-[1fr_360px] lg:items-start">
        <div className="space-y-2">
          <h1 className="text-2xl font-bold sm:text-3xl">Placement prep</h1>
          <p className="max-w-2xl text-ink-600">Mock interviews and resume reviews from seniors who cracked placements and internships, plus their round-by-round interview experiences.</p>
          {isMentor && (
            <div className="flex flex-wrap gap-3 pt-1 text-sm">
              <Link to="/prep/questions" className="link inline-flex items-center gap-1"><BookOpen className="h-4 w-4" /> Question bank</Link>
              <Link to="/resume-reviews?as=REVIEWER" className="link inline-flex items-center gap-1"><FileText className="h-4 w-4" /> Resumes to review</Link>
            </div>
          )}
        </div>
        {user && <ReadinessCard onOpen={() => setParams({ tab: 'progress' }, { replace: true })} />}
      </div>
      <Tabs value={tab} onChange={(v) => setParams({ tab: v }, { replace: true })} tabs={TABS} />
      <div className="mt-5">
        {tab === 'interviews' && <InterviewsTab />}
        {tab === 'resumes' && <ResumesTab />}
        {tab === 'experiences' && (user ? <ExperiencesTab /> : <LoginFirst what="read seniors' interview experiences" />)}
        {tab === 'progress' && (user ? <ProgressView /> : <LoginFirst what="see your progress" />)}
      </div>
    </Container>
  )
}

function LoginFirst({ what }: { what: string }) {
  return <Empty icon={<Users className="h-10 w-10" />} title={`Log in to ${what}`} action={<ButtonLink to="/login?next=/prep">Log in</ButtonLink>}>Interview experiences are shared with verified students only.</Empty>
}

function ReadinessCard({ onOpen }: { onOpen: () => void }) {
  const { label } = useCatalog()
  const { data } = useQuery<{ myProgress: Progress }>(`{ myProgress { readiness weakestCriterion suggestedInterviewType interviews { at } resumes { score } } }`)
  const p = data?.myProgress
  if (!p) return <div className="card h-[108px] animate-pulse" />
  if (p.readiness === null) {
    return (
      <div className="card flex items-center gap-4 p-4">
        <Ring value={0} />
        <div className="text-sm"><p className="font-semibold">Your readiness</p><p className="text-ink-600">Take a mock interview: your scorecards build your readiness score here.</p></div>
      </div>
    )
  }
  const latestResume = p.resumes.at(-1)
  return (
    <button type="button" onClick={onOpen} className="card flex items-center gap-4 p-4 text-left transition hover:border-brand-300">
      <Ring value={p.readiness} />
      <div className="min-w-0 space-y-0.5 text-sm">
        <p className="font-semibold">Your readiness</p>
        <p className="text-xs text-ink-500">{plural(p.interviews.length, 'mock interview')}{latestResume ? ` · resume ${latestResume.score}/10` : ''}</p>
        {p.weakestCriterion && <p>Weakest: <b>{label('feedbackCriteria', p.weakestCriterion).toLowerCase()}</b></p>}
        {p.suggestedInterviewType && <p className="font-medium text-brand-600">Next: a {label('interviewTypes', p.suggestedInterviewType)} round →</p>}
      </div>
    </button>
  )
}

function Ring({ value }: { value: number }) {
  const r = 32
  const c = 2 * Math.PI * r
  return (
    <svg viewBox="0 0 80 80" className="h-20 w-20 shrink-0" role="img" aria-label={`Readiness ${value} percent`}>
      <circle cx="40" cy="40" r={r} fill="none" stroke="var(--color-ink-100)" strokeWidth="8" />
      <circle cx="40" cy="40" r={r} fill="none" stroke="var(--color-brand-500)" strokeWidth="8" strokeLinecap="round" strokeDasharray={`${(value / 100) * c} ${c}`} transform="rotate(-90 40 40)" />
      <text x="40" y="45" textAnchor="middle" fontSize="16" fontWeight="700" fill="var(--color-ink-900)">{value ? `${value}%` : '–'}</text>
    </svg>
  )
}

function CompanyChips({ companies, value, onChange, allLabel }: { companies: { name: string; count: number }[]; value: string; onChange: (v: string) => void; allLabel: string }) {
  if (!companies.length) return null
  return (
    <div className="flex flex-wrap gap-1.5">
      <button type="button" onClick={() => onChange('')} className={cx('rounded-full border px-3 py-1 text-sm', !value ? 'border-ink-900 bg-ink-900 text-white' : 'border-ink-200 bg-white text-ink-700')}>{allLabel}</button>
      {companies.slice(0, 14).map((c) => (
        <button key={c.name} type="button" onClick={() => onChange(value.toLowerCase() === c.name.toLowerCase() ? '' : c.name)}
          className={cx('rounded-full border px-3 py-1 text-sm', value.toLowerCase() === c.name.toLowerCase() ? 'border-ink-900 bg-ink-900 text-white' : 'border-ink-200 bg-white text-ink-700 hover:border-ink-300')}>
          {c.name} <span className="opacity-60">{c.count}</span>
        </button>
      ))}
    </div>
  )
}

function InterviewsTab() {
  const { interviewTypes } = useCatalog()
  const [params, setParams] = useSearchParams()
  const company = params.get('company') ?? ''
  const type = params.get('type') ?? ''
  const page = Number(params.get('page') ?? 1)
  const [text, setText] = useState(params.get('q') ?? '')
  const q = useDebounced(text, 350)
  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params)
    if (v) p.set(k, v)
    else p.delete(k)
    if (k !== 'page') p.delete('page')
    setParams(p, { replace: true })
  }
  const { data: cos } = useQuery<{ mentorCompanies: { name: string; count: number }[] }>(`{ mentorCompanies { name count } }`)
  const { data: soon } = useQuery<{ availableSoon: { startAt: string; provider: Provider }[] }>(
    `query($c: String) { availableSoon(kind: MENTOR, company: $c, withinHours: 12, limit: 6) { startAt provider { id kind displayName avatarUrl companies interviewTypes } } }`,
    { c: company || null },
  )
  const { data, loading, error } = useQuery<{ providers: { total: number; items: Provider[] } }>(
    `query($f: ProviderFilter!, $p: Int) { providers(filter: $f, page: $p, pageSize: 12) { total items { ${MENTOR_CARD} } } }`,
    { f: { kind: 'MENTOR', company: company || null, interviewType: type || null, query: q || null }, p: page },
  )
  const { label } = useCatalog()
  return (
    <div className="space-y-5">
      <CompanyChips companies={cos?.mentorCompanies ?? []} value={company} onChange={(v) => set('company', v)} allLabel="All companies" />
      <div className="flex flex-col gap-3 sm:flex-row">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-400" />
          <Input value={text} onChange={(e) => { setText(e.target.value); if (!e.target.value) set('q', '') }} className="pl-9" placeholder="Search company, role or name" aria-label="Search mentors" />
        </div>
        <Select className="sm:w-56" aria-label="Interview type" value={type} onChange={(e) => set('type', e.target.value)}>
          <option value="">Every interview type</option>
          {interviewTypes.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </Select>
      </div>
      {!!soon?.availableSoon.length && (
        <section className="space-y-2">
          <div className="flex items-baseline gap-2"><h2 className="font-semibold">Available soon</h2><span className="text-sm text-ink-500">Open slots in the next 12 hours</span></div>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {soon.availableSoon.map((s) => (
              <Link key={s.provider.id} to={`/mentors/${s.provider.id}`} className="card flex items-center gap-3 p-3 transition hover:border-brand-300">
                <Avatar name={s.provider.displayName} url={s.provider.avatarUrl} size={40} />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-semibold">{s.provider.displayName}</p>
                  <p className="truncate text-xs text-ink-500">{[s.provider.companies[0], s.provider.interviewTypes.map((t) => label('interviewTypes', t))[0]].filter(Boolean).join(' · ')}</p>
                </div>
                <Badge tone="brand">{slotLabel(s.startAt)}</Badge>
              </Link>
            ))}
          </div>
        </section>
      )}
      <ErrorNote error={error} />
      {loading && !data ? <PageLoader /> : !data?.providers.items.length ? (
        <Empty icon={<Briefcase className="h-10 w-10" />} title="No mentors match yet" action={<ButtonLink to="/dashboard/profile?kind=MENTOR">Become a mentor</ButtonLink>}>
          Try another company or interview type. Placed or interned somewhere? Help juniors by running mock interviews.
        </Empty>
      ) : (
        <>
          <p className="text-sm text-ink-500">{plural(data.providers.total, 'mentor')}</p>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">{data.providers.items.map((p) => <ProviderCard key={p.id} p={p} />)}</div>
          <Pagination page={page} pageSize={12} total={data.providers.total} onChange={(p) => set('page', String(p))} />
        </>
      )}
    </div>
  )
}

function ResumesTab() {
  const [page, setPage] = useState(1)
  const { data, loading, error } = useQuery<{ providers: { total: number; items: Provider[] } }>(
    `query($p: Int) { providers(filter: { kind: MENTOR, resumeReviews: true }, page: $p, pageSize: 12) { total items { ${MENTOR_CARD} } } }`, { p: page })
  return (
    <div className="space-y-4">
      <div className="card flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-sm text-ink-700">Upload your resume for a role. Within the promised days you get a score out of 10, notes on every section and comments pinned to the exact lines.</p>
        <ButtonLink to="/resume-reviews" variant="secondary">My resume reviews</ButtonLink>
      </div>
      <ErrorNote error={error} />
      {loading && !data ? <PageLoader /> : !data?.providers.items.length ? (
        <Empty icon={<FileText className="h-10 w-10" />} title="No reviewers yet">Mentors who review resumes appear here.</Empty>
      ) : (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">{data.providers.items.map((p) => <ProviderCard key={p.id} p={p} />)}</div>
          <Pagination page={page} pageSize={12} total={data.providers.total} onChange={setPage} />
        </>
      )}
    </div>
  )
}

// ── Interview experiences ───────────────────────────────────────────────────

const OUTCOMES: Record<ExperienceOutcome, { label: string; tone: Tone }> = {
  SELECTED: { label: 'Selected', tone: 'green' },
  REJECTED: { label: 'Not selected', tone: 'red' },
  WAITING: { label: 'Waiting for result', tone: 'yellow' },
}

function ExperiencesTab() {
  const [company, setCompany] = useState('')
  const [page, setPage] = useState(1)
  const { data: cos } = useQuery<{ experienceCompanies: { name: string; count: number }[] }>(`{ experienceCompanies { name count } }`)
  const { data, loading, error } = useQuery<{ interviewExperiences: { total: number; items: InterviewExperience[] } }>(
    `query($c: String, $p: Int) { interviewExperiences(company: $c, page: $p, pageSize: 10) { total items { ${EXPERIENCE} } } }`, { c: company || null, p: page })
  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-sm text-ink-600">Round-by-round write-ups from seniors, checked by our team before they appear.</p>
        <ButtonLink to="/prep/experiences/new"><Plus className="h-4 w-4" /> Share your experience</ButtonLink>
      </div>
      <CompanyChips companies={cos?.experienceCompanies ?? []} value={company} onChange={(v) => { setCompany(v); setPage(1) }} allLabel="All companies" />
      <ErrorNote error={error} />
      {loading && !data ? <PageLoader /> : !data?.interviewExperiences.items.length ? (
        <Empty icon={<ClipboardCheck className="h-10 w-10" />} title="No experiences yet" action={<ButtonLink to="/prep/experiences/new">Be the first to share</ButtonLink>}>
          Sat a company's interviews? Juniors will thank you.
        </Empty>
      ) : (
        <>
          <ul className="grid gap-3 md:grid-cols-2">
            {data.interviewExperiences.items.map((e) => (
              <li key={e.id}><Link to={`/prep/experiences/${e.id}`} className="card flex h-full flex-col gap-2 p-4 transition hover:border-brand-300">
                <div className="flex items-start justify-between gap-2">
                  <div><p className="font-semibold">{e.company} · {e.role}</p><p className="text-xs text-ink-500">{e.year} · {plural(e.rounds.length, 'round')}{e.campus ? ` · ${e.campus.name}` : ''}</p></div>
                  <Badge tone={OUTCOMES[e.outcome].tone}>{OUTCOMES[e.outcome].label}</Badge>
                </div>
                <p className="line-clamp-2 text-sm text-ink-700">{e.rounds.map((r) => r.name).join(' → ')}</p>
                <p className="mt-auto text-xs text-ink-500">{e.author ? `By ${e.author.displayName}` : 'Shared anonymously'}</p>
              </Link></li>
            ))}
          </ul>
          <Pagination page={page} pageSize={10} total={data.interviewExperiences.total} onChange={setPage} />
        </>
      )}
    </div>
  )
}

export function ExperienceDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const toast = useToast()
  const { data, loading, error } = useQuery<{ interviewExperience: InterviewExperience | null }>(`query($id: ID!) { interviewExperience(id: $id) { ${EXPERIENCE} } }`, { id })
  const e = data?.interviewExperience
  useDocumentTitle(e ? `${e.company} interview experience` : '')
  const remove = useAction(async () => {
    await gql(`mutation($id: ID!) { deleteInterviewExperience(id: $id) }`, { id })
    toast('Deleted.')
    navigate('/prep?tab=experiences')
  })
  if (loading) return <PageLoader />
  if (error) return <Container><ErrorNote error={error} /></Container>
  if (!e) return <Container><Empty title="Experience not found">It may be waiting for review, or removed.</Empty></Container>
  return (
    <Container className="max-w-3xl">
      <Link to="/prep?tab=experiences" className="text-sm link">← Interview experiences</Link>
      <article className="card mt-4 space-y-5 p-6">
        <header className="space-y-2">
          <div className="flex flex-wrap items-center gap-2"><h1 className="text-2xl font-bold">{e.company}</h1><Badge tone={OUTCOMES[e.outcome].tone}>{OUTCOMES[e.outcome].label}</Badge>
            {e.status !== 'PUBLISHED' && <Badge tone={e.status === 'PENDING' ? 'yellow' : 'red'}>{e.status === 'PENDING' ? 'Waiting for review' : 'Not published'}</Badge>}</div>
          <p className="text-ink-700">{e.role} · {e.year}</p>
          <p className="flex items-center gap-2 text-sm text-ink-500">{e.author ? <><Avatar name={e.author.displayName} url={e.author.avatarUrl} size={24} /> {e.author.displayName}</> : 'Shared anonymously'}{e.campus ? ` · ${e.campus.name}` : ''} · {formatDate(e.createdAt)}</p>
          {e.status === 'REJECTED' && e.reviewReason && <ErrorNote error={new Error(`Reason: ${e.reviewReason}`)} />}
        </header>
        <ol className="space-y-4">
          {e.rounds.map((r, i) => (
            <li key={i} className="grid grid-cols-[28px_1fr] gap-3">
              <span className="grid h-7 w-7 place-items-center rounded-full bg-brand-50 text-sm font-semibold text-brand-700">{i + 1}</span>
              <div><p className="font-semibold">{r.name}</p><p className="mt-1 whitespace-pre-line text-sm text-ink-700">{r.details}</p></div>
            </li>
          ))}
        </ol>
        {e.tips && <div className="rounded-xl bg-ink-50 p-4"><p className="text-xs font-semibold uppercase tracking-wide text-ink-500">Tips</p><p className="mt-1 whitespace-pre-line text-sm text-ink-800">{e.tips}</p></div>}
        {e.mine && <div className="border-t border-ink-100 pt-4"><Button variant="ghost" className="text-red-600" loading={remove.pending} onClick={() => remove.run()}><Trash2 className="h-4 w-4" /> Delete</Button></div>}
      </article>
    </Container>
  )
}

export function NewExperience() {
  useDocumentTitle('Share an interview experience')
  const { user } = useAuth()
  const navigate = useNavigate()
  const toast = useToast()
  const year = new Date().getFullYear()
  const [form, setForm] = useState({ company: '', role: '', year, outcome: 'SELECTED' as ExperienceOutcome, tips: '', anonymous: false })
  const [rounds, setRounds] = useState([{ name: 'Online assessment', details: '' }])
  const send = useAction(async () => {
    const d = await gql<{ shareInterviewExperience: { id: string } }>(`mutation($i: ExperienceInput!) { shareInterviewExperience(input: $i) { id } }`, { i: { ...form, rounds } })
    toast('Thanks! It appears once our team has checked it.')
    navigate(`/prep/experiences/${d.shareInterviewExperience.id}`)
  })
  if (!user) return null
  const setRound = (i: number, patch: Partial<{ name: string; details: string }>) => setRounds((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  const valid = form.company.trim().length >= 2 && form.role.trim().length >= 2 && rounds.every((r) => r.name.trim().length >= 2 && r.details.trim().length >= 10)
  return (
    <Container className="max-w-3xl">
      <PageHeader title="Share an interview experience" subtitle="What each round was like, so juniors know what to expect. Our team checks every post before it appears." />
      <VerificationBanner user={user} need="campus" />
      <form className="card mt-4 space-y-5 p-6" onSubmit={(e) => { e.preventDefault(); send.run() }}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Company"><Input required maxLength={60} value={form.company} onChange={(e) => setForm({ ...form, company: e.target.value })} placeholder="e.g. Amazon" /></Field>
          <Field label="Role"><Input required maxLength={80} value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })} placeholder="e.g. SDE intern" /></Field>
          <Field label="Year"><Input type="number" min={2000} max={year + 1} value={form.year} onChange={(e) => setForm({ ...form, year: Number(e.target.value) })} /></Field>
          <Field label="Result"><Select value={form.outcome} onChange={(e) => setForm({ ...form, outcome: e.target.value as ExperienceOutcome })}>
            {(Object.keys(OUTCOMES) as ExperienceOutcome[]).map((o) => <option key={o} value={o}>{OUTCOMES[o].label}</option>)}
          </Select></Field>
        </div>
        <div className="space-y-3">
          <p className="label">Rounds, in order</p>
          {rounds.map((r, i) => (
            <div key={i} className="space-y-2 rounded-xl border border-ink-200 p-3">
              <div className="flex gap-2">
                <Input aria-label={`Round ${i + 1} name`} maxLength={60} value={r.name} onChange={(e) => setRound(i, { name: e.target.value })} placeholder={`Round ${i + 1}, e.g. Technical interview`} />
                {rounds.length > 1 && <button type="button" onClick={() => setRounds((rs) => rs.filter((_, j) => j !== i))} className="rounded-lg p-2 text-ink-400 hover:bg-ink-100 hover:text-red-600" aria-label="Remove round"><Trash2 className="h-4 w-4" /></button>}
              </div>
              <Textarea aria-label={`Round ${i + 1} details`} rows={3} maxLength={3000} value={r.details} onChange={(e) => setRound(i, { details: e.target.value })}
                placeholder="What was asked, how long it took, what the interviewer looked for." />
            </div>
          ))}
          {rounds.length < 10 && <button type="button" className="text-sm link" onClick={() => setRounds((rs) => [...rs, { name: '', details: '' }])}>+ Add a round</button>}
        </div>
        <Field label="Tips (optional)"><Textarea rows={3} maxLength={2000} value={form.tips} onChange={(e) => setForm({ ...form, tips: e.target.value })} placeholder="What you'd do differently, what to revise." /></Field>
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" className="h-4 w-4 accent-brand-500" checked={form.anonymous} onChange={(e) => setForm({ ...form, anonymous: e.target.checked })} />
          Share anonymously (our team still sees who posted it)</label>
        <ErrorNote error={send.error} />
        <Button type="submit" disabled={!valid || !user.campusVerified} loading={send.pending}>Submit for review</Button>
      </form>
    </Container>
  )
}

// ── My progress ─────────────────────────────────────────────────────────────

function Sparkline({ values }: { values: number[] }) {
  // Scores 1-5, one point per interview, left to right.
  const w = 220
  const h = 36
  const x = (i: number) => (values.length === 1 ? w / 2 : 6 + (i * (w - 12)) / (values.length - 1))
  const y = (v: number) => h - 4 - ((v - 1) / 4) * (h - 8)
  const pts = values.map((v, i) => `${x(i)},${y(v)}`).join(' ')
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="h-9 w-full" preserveAspectRatio="none" aria-hidden="true">
      {[1, 3, 5].map((g) => <line key={g} x1="0" x2={w} y1={y(g)} y2={y(g)} stroke="var(--color-ink-100)" strokeWidth="1" />)}
      <polyline points={pts} fill="none" stroke="var(--color-brand-500)" strokeWidth="2" vectorEffect="non-scaling-stroke" />
      {values.length > 0 && <circle cx={x(values.length - 1)} cy={y(values[values.length - 1])} r="3.5" fill="var(--color-brand-500)" />}
    </svg>
  )
}

const VERDICT_TONE: Record<string, Tone> = { READY: 'green', ALMOST: 'yellow', NOT_YET: 'red' }

function ProgressView() {
  const { feedbackCriteria, label } = useCatalog()
  const { data, loading, error } = useQuery<{ myProgress: Progress }>(
    `{ myProgress { readiness weakestCriterion suggestedInterviewType
        interviews { at verdict interviewType scores { criterion score } booking { id provider { id kind displayName } } }
        resumes { id at score targetRole } } }`)
  if (loading && !data) return <PageLoader />
  if (error) return <ErrorNote error={error} />
  const p = data!.myProgress
  if (!p.interviews.length && !p.resumes.length) {
    return <Empty icon={<TrendingUp className="h-10 w-10" />} title="Nothing to show yet" action={<ButtonLink to="/prep">Find a mentor</ButtonLink>}>
      Every mock interview ends with a scorecard. Your scores from each one build up here, so you can see what's improving.
    </Empty>
  }
  const latest = (c: string) => [...p.interviews].reverse().find((i) => i.scores.some((s) => s.criterion === c))?.scores.find((s) => s.criterion === c)?.score
  return (
    <div className="grid gap-5 lg:grid-cols-[1.25fr_1fr]">
      <section className="card space-y-3 p-5">
        <div className="flex items-baseline justify-between"><h2 className="font-semibold">Skills across interviews</h2><span className="text-xs text-ink-500">first → latest · 1–5</span></div>
        {feedbackCriteria.map((c) => {
          const values = p.interviews.flatMap((i) => i.scores.filter((s) => s.criterion === c.value).map((s) => s.score))
          const weak = p.weakestCriterion === c.value
          return (
            <div key={c.value} className="grid grid-cols-[130px_1fr_44px] items-center gap-3 text-sm">
              <span className={cx(weak && 'font-semibold text-amber-700')}>{c.label}</span>
              {values.length ? <Sparkline values={values} /> : <span className="text-xs text-ink-400">no scores yet</span>}
              <b className={cx('text-right tabular-nums', weak && 'text-amber-700')}>{latest(c.value) ?? '–'}/5</b>
            </div>
          )
        })}
        <p className="text-xs text-ink-500">Each line follows one skill from your first interview (left) to your latest (right). Your weakest skill is in amber.</p>
      </section>
      <div className="space-y-5">
        <section className="card space-y-2 p-5">
          <h2 className="font-semibold">Verdicts</h2>
          <ol className="space-y-1.5">
            {p.interviews.map((i) => (
              <li key={i.booking.id} className="flex items-center justify-between gap-2 text-sm">
                <Link to={`/bookings/${i.booking.id}`} className="min-w-0 truncate link">{i.interviewType ? label('interviewTypes', i.interviewType) : 'Interview'} · {i.booking.provider.displayName}</Link>
                <span className="flex shrink-0 items-center gap-2"><span className="text-xs text-ink-500">{formatDate(i.at)}</span><Badge tone={VERDICT_TONE[i.verdict]}>{label('interviewVerdicts', i.verdict)}</Badge></span>
              </li>
            ))}
            {!p.interviews.length && <li className="text-sm text-ink-500">No scored interviews yet.</li>}
          </ol>
        </section>
        <section className="card space-y-2 p-5">
          <h2 className="font-semibold">Resume score</h2>
          {p.resumes.length ? (
            <>
              <p className="text-3xl font-bold">{p.resumes.at(-1)!.score}<span className="text-base font-medium text-ink-500">/10</span>
                {p.resumes.length > 1 && <Badge tone={p.resumes.at(-1)!.score >= p.resumes[0].score ? 'green' : 'red'} className="ml-2 align-middle">{p.resumes.at(-1)!.score - p.resumes[0].score >= 0 ? '+' : ''}{p.resumes.at(-1)!.score - p.resumes[0].score} since first review</Badge>}</p>
              <ul className="text-sm text-ink-600">{p.resumes.map((r) => <li key={r.id}><Link to={`/resume-reviews/${r.id}`} className="link">{r.targetRole}</Link> · {r.score}/10 · {formatDate(r.at)}</li>)}</ul>
            </>
          ) : <p className="text-sm text-ink-500">No resume reviews yet. <Link to="/prep?tab=resumes" className="link">Get one</Link>.</p>}
        </section>
        {p.suggestedInterviewType && (
          <section className="card space-y-2 border-brand-200 bg-brand-50 p-5">
            <h2 className="font-semibold">Recommended next</h2>
            <p className="text-sm text-ink-800">A <b>{label('interviewTypes', p.suggestedInterviewType)}</b> round: {label('feedbackCriteria', p.weakestCriterion ?? '').toLowerCase()} is your lowest score right now.</p>
            <ButtonLink to={`/prep?tab=interviews&type=${p.suggestedInterviewType}`}>See {label('interviewTypes', p.suggestedInterviewType)} mentors</ButtonLink>
          </section>
        )}
      </div>
    </div>
  )
}

// ── Question bank (mentors) ─────────────────────────────────────────────────

const DIFFICULTIES: Difficulty[] = ['EASY', 'MEDIUM', 'HARD']
const DIFF_TONE: Record<Difficulty, Tone> = { EASY: 'green', MEDIUM: 'yellow', HARD: 'red' }

export function QuestionBank() {
  useDocumentTitle('Question bank')
  const { interviewTypes, label } = useCatalog()
  const toast = useToast()
  const [text, setText] = useState('')
  const [type, setType] = useState('')
  const [mine, setMine] = useState(false)
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<Question | 'new' | null>(null)
  const q = useDebounced(text, 300)
  const { data, loading, error, refetch } = useQuery<{ questions: { total: number; items: Question[] } }>(
    `query($q: String, $t: InterviewType, $m: Boolean, $p: Int) { questions(query: $q, interviewType: $t, mine: $m, page: $p, pageSize: 20) { total items { ${QUESTION} } } }`,
    { q: q || null, t: type || null, m: mine, p: page })
  const remove = useAction(async (id: string) => {
    await gql(`mutation($id: ID!) { deleteQuestion(id: $id) }`, { id })
    toast('Question deleted.')
    refetch()
  })
  const denied = (error as { reason?: string } | undefined)?.reason === 'MENTOR_REQUIRED'
  if (denied) return <Container><Empty icon={<BookOpen className="h-10 w-10" />} title="The question bank is for career mentors" action={<ButtonLink to="/dashboard/profile?kind=MENTOR">Become a mentor</ButtonLink>}>Mentors save problems here and pick them during mock interviews. Students can't see it, so interviews stay fair.</Empty></Container>
  return (
    <Container className="max-w-4xl">
      <PageHeader title="Question bank" subtitle="Problems mentors save and reuse in mock interviews. Students never see this page." actions={<Button onClick={() => setEditing('new')}><Plus className="h-4 w-4" /> Add a question</Button>} />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <Input className="sm:flex-1" placeholder="Search questions" value={text} onChange={(e) => { setText(e.target.value); setPage(1) }} aria-label="Search questions" />
        <Select className="sm:w-52" value={type} onChange={(e) => { setType(e.target.value); setPage(1) }} aria-label="Interview type">
          <option value="">Every type</option>{interviewTypes.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </Select>
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" className="h-4 w-4 accent-brand-500" checked={mine} onChange={(e) => { setMine(e.target.checked); setPage(1) }} /> Mine only</label>
      </div>
      <ErrorNote error={denied ? null : error} />
      {loading && !data ? <PageLoader /> : !data?.questions.items.length ? (
        <Empty icon={<BookOpen className="h-10 w-10" />} title="No questions yet" action={<Button onClick={() => setEditing('new')}>Add the first one</Button>}>Save the problems you like to ask; pick them from the room during an interview.</Empty>
      ) : (
        <>
          <ul className="card divide-y divide-ink-100">
            {data.questions.items.map((x) => (
              <li key={x.id} className="space-y-1 p-4">
                <div className="flex flex-wrap items-center gap-2"><b>{x.title}</b><Badge tone={DIFF_TONE[x.difficulty]}>{x.difficulty.toLowerCase()}</Badge><span className="text-xs text-ink-500">{label('interviewTypes', x.interviewType)}{x.companies.length ? ` · ${x.companies.join(', ')}` : ''}</span></div>
                <p className="line-clamp-2 whitespace-pre-line text-sm text-ink-700">{x.body}</p>
                <div className="flex gap-3 text-xs text-ink-500"><span>By {x.mine ? 'you' : x.author?.displayName}</span>
                  {x.mine && <><button type="button" className="link" onClick={() => setEditing(x)}>Edit</button><button type="button" className="font-medium text-red-600 hover:underline" onClick={() => remove.run(x.id)}>Delete</button></>}</div>
              </li>
            ))}
          </ul>
          <Pagination page={page} pageSize={20} total={data.questions.total} onChange={setPage} />
        </>
      )}
      {editing && <QuestionForm question={editing === 'new' ? null : editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); refetch() }} />}
    </Container>
  )
}

function QuestionForm({ question, onClose, onSaved }: { question: Question | null; onClose: () => void; onSaved: () => void }) {
  const { interviewTypes } = useCatalog()
  const toast = useToast()
  const [f, setF] = useState({
    title: question?.title ?? '', body: question?.body ?? '', interviewType: question?.interviewType ?? interviewTypes[0]?.value ?? 'CODING',
    difficulty: question?.difficulty ?? ('MEDIUM' as Difficulty), companies: (question?.companies ?? []).join(', '),
  })
  const save = useAction(async () => {
    await gql(`mutation($id: ID, $i: QuestionInput!) { saveQuestion(id: $id, input: $i) { id } }`, {
      id: question?.id ?? null, i: { ...f, companies: f.companies.split(',').map((c) => c.trim()).filter(Boolean) } })
    toast(question ? 'Question updated.' : 'Question added.')
    onSaved()
  })
  return (
    <Modal open onClose={onClose} title={question ? 'Edit question' : 'Add a question'}
      footer={<><Button variant="secondary" onClick={onClose}>Cancel</Button><Button loading={save.pending} disabled={f.title.trim().length < 3 || f.body.trim().length < 10} onClick={() => save.run()}>Save</Button></>}>
      <div className="space-y-3">
        <Field label="Title"><Input maxLength={150} value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} placeholder="e.g. Number of islands" /></Field>
        <Field label="Problem" hint="What the student sees in the room. Include examples and constraints."><Textarea rows={7} maxLength={10000} value={f.body} onChange={(e) => setF({ ...f, body: e.target.value })} /></Field>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Interview type"><Select value={f.interviewType} onChange={(e) => setF({ ...f, interviewType: e.target.value })}>{interviewTypes.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}</Select></Field>
          <Field label="Difficulty"><Select value={f.difficulty} onChange={(e) => setF({ ...f, difficulty: e.target.value as Difficulty })}>{DIFFICULTIES.map((d) => <option key={d} value={d}>{d.charAt(0) + d.slice(1).toLowerCase()}</option>)}</Select></Field>
        </div>
        <Field label="Asked at (optional)" hint="Comma-separated companies."><Input value={f.companies} onChange={(e) => setF({ ...f, companies: e.target.value })} placeholder="e.g. Amazon, Google" /></Field>
        <ErrorNote error={save.error} />
      </div>
    </Modal>
  )
}
