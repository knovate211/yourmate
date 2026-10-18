import { Suspense, lazy, useEffect, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ClipboardCheck, Download, FileText, Inbox, Sparkles, Trash2, Upload } from 'lucide-react'
import { Container } from '../components/Layout'
import { VerificationBanner, providerPath } from '../components/domain'
import { Avatar, Badge, Button, ButtonLink, Empty, ErrorNote, Field, Input, Modal, PageHeader, PageLoader, Pagination, Select, Tabs, Textarea, cx, type Tone } from '../components/ui'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { gql, gqlUpload } from '../lib/graphql'
import { useAction, useDocumentTitle, useQuery } from '../lib/hooks'
import { RESUME_REVIEW } from '../lib/fragments'
import { formatDate, formatDateTime, formatMoney, plural, timeAgo } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import type { Booking, InterviewFeedback, Provider, ResumeReview, ResumeReviewStatus } from '../lib/types'

const clock = (secs: number) => `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}`

// Career mentors: the resume review flow, and the scorecard a mentor fills in
// after a mock interview (mock interviews themselves are ordinary bookings).

const RESUME_TONES: Record<ResumeReviewStatus, Tone> = {
  REQUESTED: 'yellow',
  ACCEPTED: 'blue',
  DELIVERED: 'green',
  DECLINED: 'red',
  CANCELLED: 'gray',
  EXPIRED: 'gray',
}
const RESUME_LABELS: Record<ResumeReviewStatus, string> = {
  REQUESTED: 'Awaiting reply',
  ACCEPTED: 'In review',
  DELIVERED: 'Feedback ready',
  DECLINED: 'Declined',
  CANCELLED: 'Cancelled',
  EXPIRED: 'Expired',
}

function ResumeStatusBadge({ r }: { r: Pick<ResumeReview, 'status' | 'overdue'> }) {
  if (r.overdue) return <Badge tone="red">Overdue</Badge>
  return <Badge tone={RESUME_TONES[r.status]}>{RESUME_LABELS[r.status]}</Badge>
}

/** 1–5 buttons, used by the scorecard and resume section ratings. */
function ScorePicker({ value, onChange, label }: { value: number; onChange: (v: number) => void; label: string }) {
  return (
    <div className="flex gap-1" role="radiogroup" aria-label={label}>
      {[1, 2, 3, 4, 5].map((i) => (
        <button key={i} type="button" role="radio" aria-checked={value === i} onClick={() => onChange(i)}
          className={cx('h-8 w-8 rounded-lg border text-sm font-semibold transition', value === i ? 'border-brand-500 bg-brand-500 text-white' : i < value ? 'border-brand-200 bg-brand-50 text-brand-800' : 'border-ink-200 text-ink-600 hover:border-brand-300')}>
          {i}
        </button>
      ))}
    </div>
  )
}

function ScoreBar({ score, max = 5 }: { score: number; max?: number }) {
  return (
    <div className="flex items-center gap-2">
      <div className="h-2 w-28 overflow-hidden rounded-full bg-ink-100">
        <div className="h-full rounded-full bg-brand-500" style={{ width: `${(score / max) * 100}%` }} />
      </div>
      <span className="w-8 text-right text-sm font-semibold tabular-nums">{score}/{max}</span>
    </div>
  )
}

/** Star rating with a comment, posted with the given mutation. */
export function StarReview({ title, submit, onDone }: { title: string; submit: (rating: number, comment: string) => Promise<unknown>; onDone: () => void }) {
  const [rating, setRating] = useState(0)
  const [comment, setComment] = useState('')
  const toast = useToast()
  const send = useAction(async () => {
    await submit(rating, comment)
    toast('Thanks for your review!')
    onDone()
  })
  return (
    <section className="card border-brand-200 p-6">
      <h2 className="font-semibold">{title}</h2>
      <div className="mt-3 flex gap-1" role="radiogroup" aria-label="Rating">
        {[1, 2, 3, 4, 5].map((i) => (
          <button key={i} type="button" role="radio" aria-checked={rating === i} onClick={() => setRating(i)} className={cx('text-3xl leading-none transition', i <= rating ? 'text-amber-400' : 'text-ink-200 hover:text-amber-200')} aria-label={`${i} star${i > 1 ? 's' : ''}`}>
            ★
          </button>
        ))}
      </div>
      <Textarea className="mt-3" value={comment} onChange={(e) => setComment(e.target.value)} placeholder="What went well? What could be better?" maxLength={1000} rows={3} />
      <ErrorNote error={send.error} className="mt-3" />
      <Button className="mt-3" disabled={!rating} loading={send.pending} onClick={() => send.run()}>Post review</Button>
    </section>
  )
}

// ── Mock interview scorecard ────────────────────────────────────────────────

const FEEDBACK = `scores { criterion score } verdict strengths improvements resources updatedAt`

export function InterviewFeedbackCard({ booking, onSaved }: { booking: Booking; onSaved: () => void }) {
  const isMentor = booking.myRole === 'PROVIDER'
  const started = new Date(booking.startAt).getTime() <= Date.now()
  const canWrite = isMentor && (booking.status === 'COMPLETED' || (booking.status === 'ACCEPTED' && started))
  const [editing, setEditing] = useState(false)
  const f = booking.feedback

  // "Finish & write scorecard" in the room lands here.
  useEffect(() => {
    if (window.location.hash === '#scorecard') document.getElementById('scorecard')?.scrollIntoView({ block: 'start' })
  }, [])
  if (f && !editing) return <FeedbackView f={f} onEdit={isMentor ? () => setEditing(true) : undefined} />
  if (canWrite) return <FeedbackForm booking={booking} initial={f ?? undefined} onSaved={() => { setEditing(false); onSaved() }} />
  if (isMentor || booking.status === 'CANCELLED' || booking.status === 'DECLINED' || booking.status === 'EXPIRED') return null
  return (
    <section className="card flex items-start gap-3 p-5 text-sm text-ink-600">
      <ClipboardCheck className="mt-0.5 h-5 w-5 shrink-0 text-brand-500" />
      <p>After the interview, {booking.provider.displayName.split(' ')[0]} will send you a scorecard: how you did on each skill, whether you're ready, and what to practise next.</p>
    </section>
  )
}

function FeedbackView({ f, onEdit }: { f: InterviewFeedback; onEdit?: () => void }) {
  const { label } = useCatalog()
  const tone: Record<InterviewFeedback['verdict'], Tone> = { READY: 'green', ALMOST: 'yellow', NOT_YET: 'red' }
  return (
    <section className="card p-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 font-semibold"><ClipboardCheck className="h-5 w-5 text-brand-500" /> Interview scorecard</h2>
        <Badge tone={tone[f.verdict]}>{label('interviewVerdicts', f.verdict)}</Badge>
      </div>
      <ul className="mt-4 space-y-2">
        {f.scores.map((s) => (
          <li key={s.criterion} className="flex items-center justify-between gap-4 text-sm">
            <span className="text-ink-700">{label('feedbackCriteria', s.criterion)}</span>
            <ScoreBar score={s.score} />
          </li>
        ))}
      </ul>
      <div className="mt-5 grid gap-4 sm:grid-cols-2">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-emerald-700">What went well</p>
          <p className="mt-1 whitespace-pre-line text-sm text-ink-800">{f.strengths}</p>
        </div>
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-amber-700">Work on next</p>
          <p className="mt-1 whitespace-pre-line text-sm text-ink-800">{f.improvements}</p>
        </div>
      </div>
      {f.resources && (
        <div className="mt-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-ink-500">Resources</p>
          <p className="mt-1 whitespace-pre-line break-words text-sm text-ink-800">{f.resources}</p>
        </div>
      )}
      <div className="mt-4 flex items-center justify-between border-t border-ink-100 pt-3 text-xs text-ink-500">
        <span>Updated {timeAgo(f.updatedAt)}</span>
        {onEdit && <button type="button" className="link" onClick={onEdit}>Edit scorecard</button>}
      </div>
    </section>
  )
}

function FeedbackForm({ booking, initial, onSaved }: { booking: Booking; initial?: InterviewFeedback; onSaved: () => void }) {
  const { feedbackCriteria, interviewVerdicts } = useCatalog()
  const toast = useToast()
  // A new scorecard starts from what the mentor scored and noted in the room.
  const notes = booking.mentorNotes ?? []
  const noteLines = (tone: string) => notes.filter((n) => n.tone === tone).map((n) => `• ${clock(n.atSeconds)} ${n.body}`).join('\n')
  const drafted = (initial?.scores ?? booking.room?.draftScores ?? []).map((s) => [s.criterion, s.score])
  const [scores, setScores] = useState<Record<string, number>>(Object.fromEntries(drafted))
  const [verdict, setVerdict] = useState(initial?.verdict ?? '')
  const [strengths, setStrengths] = useState(initial?.strengths ?? noteLines('PLUS'))
  const [improvements, setImprovements] = useState(initial?.improvements ?? noteLines('MINUS'))
  const [resources, setResources] = useState(initial?.resources ?? '')
  const save = useAction(async () => {
    await gql(`mutation($i: InterviewFeedbackInput!) { saveInterviewFeedback(input: $i) { ${FEEDBACK} } }`, {
      i: { bookingId: booking.id, verdict, strengths, improvements, resources, scores: Object.entries(scores).map(([criterion, score]) => ({ criterion, score })) },
    })
    toast(initial ? 'Scorecard updated.' : `Scorecard sent to ${booking.student.displayName.split(' ')[0]}.`)
    onSaved()
  })
  const complete = feedbackCriteria.every((c) => scores[c.value]) && verdict && strengths.trim().length >= 10 && improvements.trim().length >= 10
  const other = notes.filter((n) => n.tone === 'NOTE')
  return (
    <form id="scorecard" className="card scroll-mt-20 space-y-5 border-brand-200 p-6" onSubmit={(e) => { e.preventDefault(); save.run() }}>
      <div>
        <h2 className="flex items-center gap-2 font-semibold"><ClipboardCheck className="h-5 w-5 text-brand-500" /> Scorecard for {booking.student.displayName}</h2>
        <p className="mt-1 text-sm text-ink-600">Score each skill from 1 (needs a lot of work) to 5 (excellent). Be specific: this is what they'll practise from.</p>
      </div>
      <ul className="space-y-3">
        {feedbackCriteria.map((c) => (
          <li key={c.value} className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <span className="text-sm font-medium">{c.label}</span>
            <ScorePicker label={c.label} value={scores[c.value] ?? 0} onChange={(v) => setScores({ ...scores, [c.value]: v })} />
          </li>
        ))}
      </ul>
      <div>
        <p className="label">Verdict</p>
        <div className="flex flex-wrap gap-2">
          {interviewVerdicts.map((v) => (
            <button key={v.value} type="button" onClick={() => setVerdict(v.value as InterviewFeedback['verdict'])}
              className={cx('rounded-full border px-3 py-1 text-sm font-medium', verdict === v.value ? 'border-brand-500 bg-brand-500 text-white' : 'border-ink-200 text-ink-700')}>
              {v.label}
            </button>
          ))}
        </div>
      </div>
      {other.length > 0 && (
        <div className="rounded-xl bg-ink-50 p-3 text-sm">
          <p className="text-xs font-semibold uppercase tracking-wide text-ink-500">Your other notes from the room</p>
          <ul className="mt-1 space-y-0.5">{other.map((n) => <li key={n.id}><b className="tabular-nums text-brand-700">{clock(n.atSeconds)}</b> {n.body}</li>)}</ul>
        </div>
      )}
      <Field label="What went well" hint="At least 10 characters.">
        <Textarea required rows={3} maxLength={2000} value={strengths} onChange={(e) => setStrengths(e.target.value)} placeholder="e.g. Clarified requirements first; clean, readable code." />
      </Field>
      <Field label="What to work on next" hint="At least 10 characters.">
        <Textarea required rows={3} maxLength={2000} value={improvements} onChange={(e) => setImprovements(e.target.value)} placeholder="e.g. State the time complexity before coding; test edge cases out loud." />
      </Field>
      <Field label="Resources (optional)" hint="Problems, articles or videos to practise with.">
        <Textarea rows={2} maxLength={1000} value={resources} onChange={(e) => setResources(e.target.value)} />
      </Field>
      <ErrorNote error={save.error} />
      <Button type="submit" disabled={!complete} loading={save.pending}>{initial ? 'Update scorecard' : 'Send scorecard'}</Button>
    </form>
  )
}

// ── Requesting a resume review ──────────────────────────────────────────────

export function ResumeRequestPanel({ p }: { p: Provider }) {
  const { user } = useAuth()
  const navigate = useNavigate()
  const toast = useToast()
  const [role, setRole] = useState('')
  const [notes, setNotes] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const send = useAction(async () => {
    const d = await gqlUpload<{ requestResumeReview: { id: string } }>(
      `mutation($i: ResumeReviewRequestInput!) { requestResumeReview(input: $i) { id } }`,
      { i: { mentorId: p.id, targetRole: role, notes } },
      { 'i.resume': file! },
    )
    toast('Resume sent! You’ll be notified when they accept.')
    navigate(`/resume-reviews/${d.requestResumeReview.id}`)
  })
  const offer = p.resumeOffer
  if (!offer) return null
  const tooBig = !!file && file.size > 5 * 1024 * 1024
  return (
    <div className="space-y-4">
      <div className="flex items-baseline justify-between">
        <p className="text-2xl font-bold">{formatMoney(offer.price)}</p>
        <span className="text-sm text-ink-500">per review · {plural(offer.turnaroundDays, 'day')}</span>
      </div>
      <ol className="space-y-1 text-xs text-ink-600">
        <li>1. Upload your resume and say which role it's for.</li>
        <li>2. {p.displayName.split(' ')[0]} accepts and sends feedback within {plural(offer.turnaroundDays, 'day')}.</li>
        <li>3. You get a score, notes on each section and, often, an annotated copy.</li>
      </ol>
      <Field label="Role you're applying for">
        <Input value={role} onChange={(e) => setRole(e.target.value)} maxLength={100} placeholder="e.g. SDE intern, data analyst, product intern" />
      </Field>
      <label className="flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed border-ink-300 px-4 py-6 text-center hover:border-brand-400">
        <Upload className="h-5 w-5 text-ink-400" />
        <span className="mt-1 text-sm font-medium">{file ? file.name : 'Choose your resume (PDF, max 5 MB)'}</span>
        <input type="file" accept="application/pdf" className="sr-only" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
      </label>
      {tooBig && <p className="text-xs text-red-600">That file is larger than 5 MB.</p>}
      <Field label="Anything to focus on? (optional)">
        <Textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={3} maxLength={1000} placeholder="e.g. I'm switching from mechanical to software roles." />
      </Field>
      {user && <VerificationBanner user={user} need="campus" />}
      <ErrorNote error={send.error} />
      {user ? (
        <Button className="w-full" size="lg" disabled={!file || tooBig || role.trim().length < 2 || !user.campusVerified} loading={send.pending} onClick={() => send.run()}>
          <FileText className="h-4 w-4" /> Request review
        </Button>
      ) : (
        <ButtonLink to={`/login?next=${encodeURIComponent(providerPath(p))}`} className="w-full" size="lg">Log in to request</ButtonLink>
      )}
      <p className="text-xs text-ink-500">Only you and {p.displayName.split(' ')[0]} can open your resume. It's deleted 30 days after the review closes.</p>
    </div>
  )
}

// ── Resume reviews: list ────────────────────────────────────────────────────

const FILTERS: { value: string; label: string; statuses: ResumeReviewStatus[] | null }[] = [
  { value: 'open', label: 'In progress', statuses: ['REQUESTED', 'ACCEPTED'] },
  { value: 'done', label: 'Delivered', statuses: ['DELIVERED'] },
  { value: 'closed', label: 'Closed', statuses: ['DECLINED', 'CANCELLED', 'EXPIRED'] },
  { value: 'all', label: 'All', statuses: null },
]

export function ResumeReviews() {
  useDocumentTitle('Resume reviews')
  const [params, setParams] = useSearchParams()
  const role = (params.get('as') ?? 'STUDENT') as 'STUDENT' | 'REVIEWER'
  const filter = FILTERS.find((f) => f.value === params.get('show')) ?? FILTERS[0]
  const page = Number(params.get('page') ?? 1)
  const { data: mine } = useQuery<{ myProviderProfiles: { kind: string; resumeOffer: unknown }[] }>(`{ myProviderProfiles { kind resumeOffer { turnaroundDays } } }`)
  const isMentor = mine?.myProviderProfiles.some((p) => p.kind === 'MENTOR')
  const { data, loading, error } = useQuery<{ resumeReviews: { total: number; items: ResumeReview[] } }>(
    `query($as: ResumeReviewRole!, $s: [ResumeReviewStatus!], $p: Int) { resumeReviews(as: $as, statuses: $s, page: $p, pageSize: 20) { total items { ${RESUME_REVIEW} } } }`,
    { as: role, s: filter.statuses, p: page },
    { pollMs: 60_000 },
  )
  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params)
    p.set(k, v)
    if (k !== 'page') p.delete('page')
    setParams(p, { replace: true })
  }
  return (
    <Container className="max-w-4xl">
      <PageHeader title="Resume reviews" subtitle="Feedback on your resume from seniors who've been through placements." actions={<ButtonLink to="/prep?tab=resumes" variant="secondary">Find a reviewer</ButtonLink>} />
      {isMentor && (
        <div className="mb-4">
          <Tabs value={role} onChange={(v) => set('as', v)} tabs={[{ value: 'STUDENT', label: 'My resumes' }, { value: 'REVIEWER', label: 'To review' }]} />
        </div>
      )}
      <div className="mb-4 flex flex-wrap gap-2">
        {FILTERS.map((f) => (
          <button key={f.value} type="button" onClick={() => set('show', f.value)}
            className={cx('rounded-full border px-3 py-1 text-sm font-medium', f.value === filter.value ? 'border-brand-500 bg-brand-50 text-brand-800' : 'border-ink-200 text-ink-700')}>
            {f.label}
          </button>
        ))}
      </div>
      <ErrorNote error={error} />
      {loading && !data ? (
        <PageLoader />
      ) : !data?.resumeReviews.items.length ? (
        <Empty icon={<FileText className="h-10 w-10" />} title={role === 'REVIEWER' ? 'No resumes here' : 'No resume reviews here'}
          action={role === 'STUDENT' ? <ButtonLink to="/prep?tab=resumes">Get your resume reviewed</ButtonLink> : undefined}>
          {role === 'REVIEWER' ? 'Requests from students appear here.' : 'A senior who has been placed reads your resume and tells you exactly what to fix.'}
        </Empty>
      ) : (
        <>
          <div className="card divide-y divide-ink-100 overflow-hidden">
            {data.resumeReviews.items.map((r) => {
              const other = r.myRole === 'REVIEWER' ? { name: r.student.displayName, url: r.student.avatarUrl } : { name: r.mentor.displayName, url: r.mentor.avatarUrl }
              return (
                <Link key={r.id} to={`/resume-reviews/${r.id}`} className="flex items-center gap-3 px-4 py-3 transition hover:bg-ink-50">
                  <Avatar name={other.name} url={other.url} size={40} />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-semibold">{r.targetRole} <span className="font-normal text-ink-500">· {r.myRole === 'REVIEWER' ? 'from' : 'by'} {other.name}</span></p>
                    <p className="text-xs text-ink-500">
                      {r.status === 'ACCEPTED' && r.dueAt ? `Due ${formatDateTime(r.dueAt)}` : r.deliveredAt ? `Delivered ${formatDate(r.deliveredAt)}` : `Sent ${timeAgo(r.createdAt)}`}
                      {r.overallScore ? ` · ${r.overallScore}/10` : ''}
                    </p>
                  </div>
                  <ResumeStatusBadge r={r} />
                </Link>
              )
            })}
          </div>
          <Pagination page={page} pageSize={20} total={data.resumeReviews.total} onChange={(p) => set('page', String(p))} />
        </>
      )}
    </Container>
  )
}

// ── Resume reviews: detail ──────────────────────────────────────────────────

const RESUME_DETAIL = `${RESUME_REVIEW} fileUrl feedbackFileUrl summary sections { section rating comment } cancelledBy { id displayName }
  pins { page x y section comment }`

const PdfPins = lazy(() => import('../components/PdfPins'))

type ResumeAction = 'ACCEPT' | 'DECLINE' | 'CANCEL'

export function ResumeReviewDetail() {
  const { id = '' } = useParams()
  const toast = useToast()
  const { data, loading, error, refetch } = useQuery<{ resumeReview: ResumeReview | null }>(`query($id: ID!) { resumeReview(id: $id) { ${RESUME_DETAIL} } }`, { id }, { pollMs: 30_000 })
  const [action, setAction] = useState<ResumeAction | null>(null)
  const [reason, setReason] = useState('')
  const r = data?.resumeReview
  useDocumentTitle(r ? `Resume review · ${r.targetRole}` : '')
  const transition = useAction(async () => {
    await gql(`mutation($id: ID!, $a: ResumeReviewAction!, $r: String) { transitionResumeReview(id: $id, action: $a, reason: $r) { id } }`, { id, a: action, r: reason || null })
    toast({ ACCEPT: 'Accepted. The due date starts now.', DECLINE: 'Request declined.', CANCEL: 'Cancelled.' }[action!])
    setAction(null)
    setReason('')
    refetch()
  })
  if (loading && !data) return <PageLoader />
  if (error) return <Container><ErrorNote error={error} /></Container>
  if (!r) return <Container><Empty title="Resume review not found">It may belong to someone else.</Empty></Container>

  const isReviewer = r.myRole === 'REVIEWER'
  const canCancel = (r.status === 'REQUESTED' && !isReviewer) || (r.status === 'ACCEPTED' && (isReviewer || r.overdue))
  const copy: Record<ResumeAction, { title: string; button: string; danger?: boolean }> = {
    ACCEPT: { title: 'Accept this resume', button: 'Accept' },
    DECLINE: { title: 'Decline this request', button: 'Decline', danger: true },
    CANCEL: { title: 'Cancel this review', button: 'Cancel review', danger: true },
  }
  return (
    <Container className="max-w-4xl">
      <Link to={`/resume-reviews${isReviewer ? '?as=REVIEWER' : ''}`} className="text-sm link">← All resume reviews</Link>
      <div className="mt-4 grid gap-6 md:grid-cols-[1fr_300px]">
        <div className="space-y-6">
          <section className="card p-6">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p className="text-sm text-ink-500">Resume review</p>
                <h1 className="text-xl font-bold">{r.targetRole}</h1>
              </div>
              <ResumeStatusBadge r={r} />
            </div>
            {r.statusReason && <p className="mt-3 rounded-lg bg-ink-50 px-3 py-2 text-sm text-ink-700">{r.cancelledBy ? `${r.cancelledBy.displayName}: ` : ''}{r.statusReason}</p>}
            <dl className="mt-5 grid gap-4 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-ink-500">Resume</dt>
                <dd className="font-medium">
                  {r.fileUrl ? (
                    <a href={r.fileUrl} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 link break-all"><Download className="h-4 w-4 shrink-0" /> {r.fileName}</a>
                  ) : (
                    <span className="text-ink-500">{r.fileName} (deleted)</span>
                  )}
                </dd>
              </div>
              <div>
                <dt className="text-ink-500">Price</dt>
                <dd className="font-medium">{formatMoney(r.price)} <span className="font-normal text-ink-500">· paid directly</span></dd>
              </div>
              <div>
                <dt className="text-ink-500">{r.dueAt ? 'Due' : 'Turnaround'}</dt>
                <dd className={cx('font-medium', r.overdue && 'text-red-600')}>{r.dueAt ? formatDateTime(r.dueAt) : `${plural(r.turnaroundDays, 'day')} after accepting`}</dd>
              </div>
              <div>
                <dt className="text-ink-500">Sent</dt>
                <dd className="font-medium">{formatDateTime(r.createdAt)}</dd>
              </div>
            </dl>
            {r.notes && (
              <div className="mt-5">
                <p className="text-sm text-ink-500">Note from {isReviewer ? r.student.displayName : 'you'}</p>
                <p className="mt-1 whitespace-pre-line text-sm text-ink-800">{r.notes}</p>
              </div>
            )}
            {(isReviewer && r.status === 'REQUESTED') || canCancel ? (
              <div className="mt-6 flex flex-wrap gap-2 border-t border-ink-100 pt-4">
                {isReviewer && r.status === 'REQUESTED' && (
                  <>
                    <Button onClick={() => setAction('ACCEPT')}>Accept</Button>
                    <Button variant="secondary" onClick={() => setAction('DECLINE')}>Decline</Button>
                  </>
                )}
                {canCancel && <Button variant="ghost" className="text-red-600" onClick={() => setAction('CANCEL')}>{r.status === 'REQUESTED' ? 'Withdraw request' : 'Cancel review'}</Button>}
              </div>
            ) : null}
            {!isReviewer && r.status === 'ACCEPTED' && !r.overdue && <p className="mt-4 text-xs text-ink-500">Your mentor is reviewing it. If it goes past the due date, you can cancel.</p>}
          </section>

          {r.status === 'DELIVERED' && <ResumeFeedbackView r={r} />}
          {isReviewer && r.status === 'ACCEPTED' && <DeliverForm r={r} onDone={refetch} />}
          {!isReviewer && r.status === 'DELIVERED' && !r.rated && (
            <StarReview title={`How helpful was ${r.mentor.displayName}'s review?`} onDone={refetch}
              submit={(rating, comment) => gql(`mutation($id: ID!, $r: Int!, $c: String) { rateResumeReview(id: $id, rating: $r, comment: $c) { id } }`, { id: r.id, r: rating, c: comment })} />
          )}
        </div>
        <aside className="space-y-4">
          <section className="card p-5">
            <p className="text-xs font-semibold uppercase tracking-wide text-ink-500">{isReviewer ? 'Student' : 'Mentor'}</p>
            <div className="mt-3 flex items-center gap-3">
              <Avatar name={isReviewer ? r.student.displayName : r.mentor.displayName} url={isReviewer ? r.student.avatarUrl : r.mentor.avatarUrl} size={44} />
              <div className="min-w-0">
                <p className="font-semibold">{isReviewer ? r.student.displayName : r.mentor.displayName}</p>
                <Link to={isReviewer ? `/users/${r.student.id}` : providerPath(r.mentor)} className="text-xs link">View profile</Link>
              </div>
            </div>
          </section>
          <section className="card p-5 text-xs text-ink-600">
            <p className="font-semibold text-ink-800">Good to know</p>
            <ul className="mt-2 list-disc space-y-1 pl-4">
              <li>Unanswered requests expire after 3 days.</li>
              <li>Agree on payment directly; Your Mate doesn't charge.</li>
              <li>Resume files are deleted 30 days after the review closes.</li>
            </ul>
          </section>
        </aside>
      </div>

      <Modal open={!!action} onClose={() => setAction(null)} title={action ? copy[action].title : ''}
        footer={
          <>
            <Button variant="secondary" onClick={() => setAction(null)}>Back</Button>
            {action && <Button variant={copy[action].danger ? 'danger' : 'primary'} loading={transition.pending} onClick={() => transition.run()}>{copy[action].button}</Button>}
          </>
        }>
        <div className="space-y-4">
          {action === 'ACCEPT' && <p className="text-sm text-ink-600">You'll have {plural(r.turnaroundDays, 'day')} from now to send your feedback.</p>}
          {(action === 'DECLINE' || action === 'CANCEL') && (
            <Field label={action === 'CANCEL' && isReviewer && r.status === 'ACCEPTED' ? 'Reason (required)' : 'Reason (optional)'} hint="Shared with the other person.">
              <Textarea value={reason} onChange={(e) => setReason(e.target.value)} rows={3} maxLength={500} />
            </Field>
          )}
          <ErrorNote error={transition.error} />
        </div>
      </Modal>
    </Container>
  )
}

function ResumeFeedbackView({ r }: { r: ResumeReview }) {
  const { label } = useCatalog()
  return (
    <section className="card p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="flex items-center gap-2 font-semibold"><Sparkles className="h-5 w-5 text-brand-500" /> Feedback</h2>
        {r.overallScore && <span className="rounded-xl bg-brand-50 px-3 py-1 text-lg font-bold text-brand-800">{r.overallScore}<span className="text-sm font-medium text-brand-600">/10</span></span>}
      </div>
      <p className="mt-3 whitespace-pre-line text-sm text-ink-800">{r.summary}</p>
      <ul className="mt-5 divide-y divide-ink-100 border-t border-ink-100">
        {r.sections.map((s) => (
          <li key={s.section} className="py-3">
            <div className="flex items-center justify-between gap-4">
              <span className="text-sm font-semibold">{label('resumeSections', s.section)}</span>
              <ScoreBar score={s.rating} />
            </div>
            <p className="mt-1 whitespace-pre-line text-sm text-ink-700">{s.comment}</p>
          </li>
        ))}
      </ul>
      {r.pins && r.pins.length > 0 && <PinnedComments r={r} />}
      {r.feedbackFileUrl && (
        <a href={r.feedbackFileUrl} target="_blank" rel="noopener noreferrer" className="mt-4 inline-flex items-center gap-1 text-sm link"><Download className="h-4 w-4" /> Annotated copy (PDF)</a>
      )}
    </section>
  )
}

function PinnedComments({ r }: { r: ResumeReview }) {
  const { label } = useCatalog()
  const [selected, setSelected] = useState<number | null>(null)
  const pins = r.pins ?? []
  return (
    <div className="mt-5 border-t border-ink-100 pt-5">
      <h3 className="text-sm font-semibold">Comments on your resume</h3>
      <div className="mt-3 grid gap-4 md:grid-cols-[1.1fr_1fr]">
        {r.fileUrl ? (
          <Suspense fallback={<div className="h-64 rounded-xl bg-ink-100" />}><PdfPins url={r.fileUrl} pins={pins} selected={selected} onSelect={setSelected} /></Suspense>
        ) : <p className="text-sm text-ink-500">The resume file has been deleted; the comments remain.</p>}
        <ol className="space-y-2">
          {pins.map((p, i) => (
            <li key={i}><button type="button" onClick={() => setSelected(i)} className={cx('w-full rounded-xl border p-3 text-left text-sm', selected === i ? 'border-brand-400 bg-brand-50' : 'border-ink-200 bg-white')}>
              <span className="mr-2 inline-grid h-5 w-5 place-items-center rounded-full bg-brand-500 text-[11px] font-bold text-white">{i + 1}</span>
              {p.section && <b className="mr-1">{label('resumeSections', p.section)}:</b>}{p.comment}
            </button></li>
          ))}
        </ol>
      </div>
    </div>
  )
}

function DeliverForm({ r, onDone }: { r: ResumeReview; onDone: () => void }) {
  const { resumeSections } = useCatalog()
  const toast = useToast()
  const [score, setScore] = useState(0)
  const [summary, setSummary] = useState('')
  const [notes, setNotes] = useState<Record<string, { rating: number; comment: string }>>({})
  const [file, setFile] = useState<File | null>(null)
  const [pins, setPins] = useState<{ page: number; x: number; y: number; section: string; comment: string }[]>([])
  const [selected, setSelected] = useState<number | null>(null)
  const filled = Object.entries(notes).filter(([, n]) => n.rating && n.comment.trim())
  const send = useAction(async () => {
    const i = {
      id: r.id, overallScore: score, summary, sections: filled.map(([section, n]) => ({ section, rating: n.rating, comment: n.comment })),
      pins: pins.filter((p) => p.comment.trim()).map((p) => ({ ...p, section: p.section || null })),
    }
    const q = `mutation($i: DeliverResumeReviewInput!) { deliverResumeReview(input: $i) { id } }`
    if (file) await gqlUpload(q, { i }, { 'i.annotated': file })
    else await gql(q, { i })
    toast(`Feedback sent to ${r.student.displayName.split(' ')[0]}.`)
    onDone()
  })
  const set = (section: string, patch: Partial<{ rating: number; comment: string }>) =>
    setNotes((ns) => ({ ...ns, [section]: { ...(ns[section] ?? { rating: 0, comment: '' }), ...patch } }))
  return (
    <form className="card space-y-5 border-brand-200 p-6" onSubmit={(e) => { e.preventDefault(); send.run() }}>
      <div>
        <h2 className="flex items-center gap-2 font-semibold"><Inbox className="h-5 w-5 text-brand-500" /> Your feedback</h2>
        <p className="mt-1 text-sm text-ink-600">Rate and comment on at least 3 sections. Point to specific lines and say what to change.</p>
      </div>
      <Field label="Overall score">
        <Select value={score} onChange={(e) => setScore(Number(e.target.value))} className="w-40">
          <option value={0}>Choose…</option>
          {Array.from({ length: 10 }, (_, i) => i + 1).map((n) => <option key={n} value={n}>{n} / 10</option>)}
        </Select>
      </Field>
      <Field label="Summary" hint={`${summary.trim().length}/3000 · At least 30 characters. The big picture: is it ready, and the top three changes.`}>
        <Textarea rows={4} maxLength={3000} value={summary} onChange={(e) => setSummary(e.target.value)} />
      </Field>
      <div className="space-y-4">
        {resumeSections.map((s) => (
          <div key={s.value} className="rounded-xl border border-ink-200 p-4">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <span className="text-sm font-semibold">{s.label}</span>
              <ScorePicker label={s.label} value={notes[s.value]?.rating ?? 0} onChange={(v) => set(s.value, { rating: v })} />
            </div>
            <Textarea className="mt-2" rows={2} maxLength={1500} placeholder="Skip if it's fine as is." value={notes[s.value]?.comment ?? ''} onChange={(e) => set(s.value, { comment: e.target.value })} />
          </div>
        ))}
      </div>
      {r.fileUrl && (
        <div className="space-y-3">
          <div>
            <p className="label">Pin comments to the resume (optional)</p>
            <p className="text-sm text-ink-600">Click the exact line you mean, then say what to change there.</p>
          </div>
          <div className="grid gap-4 md:grid-cols-[1.1fr_1fr]">
            <Suspense fallback={<div className="h-64 rounded-xl bg-ink-100" />}>
              <PdfPins url={r.fileUrl} pins={pins} editable selected={selected} onSelect={setSelected}
                onAdd={(page, x, y) => { if (pins.length < 40) { setPins((ps) => [...ps, { page, x, y, section: '', comment: '' }]); setSelected(pins.length) } }} />
            </Suspense>
            <ol className="space-y-2">
              {pins.length === 0 && <li className="text-sm text-ink-500">No pins yet.</li>}
              {pins.map((p, i) => (
                <li key={i} className={cx('space-y-2 rounded-xl border p-3', selected === i ? 'border-brand-400 bg-brand-50' : 'border-ink-200')}>
                  <div className="flex items-center gap-2">
                    <span className="grid h-5 w-5 place-items-center rounded-full bg-brand-500 text-[11px] font-bold text-white">{i + 1}</span>
                    <Select className="flex-1" aria-label={`Section for comment ${i + 1}`} value={p.section} onChange={(e) => setPins((ps) => ps.map((x, j) => (j === i ? { ...x, section: e.target.value } : x)))}>
                      <option value="">Any section</option>{resumeSections.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
                    </Select>
                    <button type="button" className="rounded-lg p-1.5 text-ink-400 hover:bg-ink-100 hover:text-red-600" aria-label={`Remove comment ${i + 1}`}
                      onClick={() => { setPins((ps) => ps.filter((_, j) => j !== i)); setSelected(null) }}><Trash2 className="h-4 w-4" /></button>
                  </div>
                  <Textarea rows={2} maxLength={1500} aria-label={`Comment ${i + 1}`} value={p.comment} onFocus={() => setSelected(i)}
                    onChange={(e) => setPins((ps) => ps.map((x, j) => (j === i ? { ...x, comment: e.target.value } : x)))} placeholder="What to change here" />
                </li>
              ))}
            </ol>
          </div>
        </div>
      )}
      <label className="flex cursor-pointer items-center gap-2 text-sm">
        <Upload className="h-4 w-4 text-ink-400" />
        <span className="link">{file ? file.name : 'Attach an annotated PDF (optional)'}</span>
        <input type="file" accept="application/pdf" className="sr-only" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
      </label>
      <ErrorNote error={send.error} />
      <Button type="submit" disabled={!score || summary.trim().length < 30 || filled.length < 3} loading={send.pending}>Send feedback</Button>
    </form>
  )
}
