import { useEffect, useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { Bell, Briefcase, CalendarClock, CalendarDays, Compass, GraduationCap, Inbox, Mail, MapPin, Phone, Send, Video } from 'lucide-react'
import { Container } from '../components/Layout'
import { JoinedCommunitiesCard } from './communities'
import { InterviewFeedbackCard, StarReview } from './careers'
import { RoomCard } from '../components/RoomCard'
import { BookingStatusBadge, ReportButton, VerificationBanner, providerPath } from '../components/domain'
import { Avatar, Badge, Button, ButtonLink, Empty, ErrorNote, Field, Input, Modal, PageHeader, PageLoader, Pagination, Tabs, Textarea, cx } from '../components/ui'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { gql } from '../lib/graphql'
import { useAction, useDocumentTitle, useQuery } from '../lib/hooks'
import { BOOKING_CARD, ROOM } from '../lib/fragments'
import { browserTimeZone, plural, formatDateTime, formatMoney, formatTime, timeAgo, zoneLabel } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import type { Booking, BookingStatus, Message, Notification, Provider, Verification } from '../lib/types'

const KIND_SESSION = { TUTOR: 'Tutoring session', GUIDE: 'Guide session', MENTOR: 'Mock interview' } as const
const KIND_ROLE = { TUTOR: 'Tutor', GUIDE: 'Guide', MENTOR: 'Mentor' } as const
const editPath = (kind: Provider['kind']) => `/dashboard/profile${kind === 'TUTOR' ? '' : `?kind=${kind}`}`

function sessionTitle(b: Booking, label: ReturnType<typeof useCatalog>['label']) {
  if (b.subject) return b.subject.name
  if (b.interviewType) return `${label('interviewTypes', b.interviewType)} interview`
  if (b.service) return label('guideServices', b.service)
  return KIND_SESSION[b.kind]
}

function BookingRow({ b }: { b: Booking }) {
  const { label } = useCatalog()
  const other = b.myRole === 'PROVIDER' ? { name: b.student.displayName, url: b.student.avatarUrl } : { name: b.provider.displayName, url: b.provider.avatarUrl }
  const what = sessionTitle(b, label)
  return (
    <Link to={`/bookings/${b.id}`} className="flex items-center gap-3 px-4 py-3 transition hover:bg-ink-50">
      <Avatar name={other.name} url={other.url} size={40} />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold text-ink-900">
          {what} <span className="font-normal text-ink-500">· {b.myRole === 'PROVIDER' ? 'with' : 'by'} {other.name}</span>
        </p>
        <p className="text-xs text-ink-500">
          {formatDateTime(b.startAt, b.timezone)} {zoneLabel(b.timezone)} · {b.durationMinutes} min · {label('sessionModes', b.mode)}
        </p>
      </div>
      <div className="flex flex-col items-end gap-1">
        <BookingStatusBadge status={b.status} />
        <span className="text-xs font-medium text-ink-600">{formatMoney(b.price)}</span>
      </div>
    </Link>
  )
}

export function Dashboard() {
  useDocumentTitle('Dashboard')
  const { user } = useAuth()
  const { data, loading } = useQuery<{
    asProvider: { total: number; items: Booking[] }
    upcoming: { items: Booking[] }
    myProviderProfiles: Provider[]
    myVerification: Verification
  }>(
    `{ asProvider: bookings(as: PROVIDER, statuses: [REQUESTED], upcoming: true, pageSize: 10) { total items { ${BOOKING_CARD} } }
       upcoming: bookings(as: ANY, statuses: [ACCEPTED, REQUESTED], upcoming: true, pageSize: 10) { items { ${BOOKING_CARD} } }
       myProviderProfiles { id kind status headline ratingAvg ratingCount sessionsCompleted }
       myVerification { status reason } }`,
    undefined,
    { pollMs: 60_000 },
  )
  if (!user) return null
  const profiles = data?.myProviderProfiles ?? []
  // Requests waiting for this user's answer are already listed above.
  const waiting = new Set(data?.asProvider.items.map((b) => b.id))
  const upcoming = data?.upcoming.items.filter((b) => !waiting.has(b.id)) ?? []
  return (
    <Container>
      <PageHeader title={`Hi, ${user.firstName}`} subtitle={user.campus?.name} actions={<ButtonLink to="/tutors" variant="secondary">Find a tutor</ButtonLink>} />
      <div className="mb-6 space-y-3">
        <VerificationBanner user={user} need="campus" />
        {data?.myVerification.status === 'PENDING' && (
          <div className="rounded-xl border border-sky-200 bg-sky-50 p-4 text-sm text-sky-900">Your student ID is waiting for review. We'll notify you when it's checked.</div>
        )}
      </div>
      {loading && !data ? (
        <PageLoader />
      ) : (
        <div className="grid gap-6 lg:grid-cols-[1fr_340px]">
          <div className="space-y-6">
            {(data?.asProvider.total ?? 0) > 0 && (
              <section className="card overflow-hidden border-brand-200">
                <div className="flex items-center justify-between border-b border-ink-100 bg-brand-50 px-4 py-3">
                  <h2 className="flex items-center gap-2 font-semibold text-brand-900"><Inbox className="h-4 w-4" /> Requests waiting for you</h2>
                  <Badge tone="brand">{data!.asProvider.total}</Badge>
                </div>
                <div className="divide-y divide-ink-100">{data!.asProvider.items.map((b) => <BookingRow key={b.id} b={b} />)}</div>
              </section>
            )}
            <section className="card overflow-hidden">
              <div className="flex items-center justify-between border-b border-ink-100 px-4 py-3">
                <h2 className="flex items-center gap-2 font-semibold"><CalendarClock className="h-4 w-4 text-brand-500" /> Upcoming sessions</h2>
                <Link to="/bookings" className="text-sm link">All bookings</Link>
              </div>
              {upcoming.length ? (
                <div className="divide-y divide-ink-100">{upcoming.map((b) => <BookingRow key={b.id} b={b} />)}</div>
              ) : (
                <p className="px-4 py-8 text-center text-sm text-ink-500">Nothing scheduled. <Link to="/tutors" className="link">Find a tutor</Link> or <Link to="/guides" className="link">a guide</Link>.</p>
              )}
            </section>
          </div>
          <aside className="space-y-4">
            <section className="card p-5">
              <h2 className="font-semibold">Earn on Your Mate</h2>
              {profiles.length === 0 ? (
                <>
                  <p className="mt-1 text-sm text-ink-600">Teach what you know or help newcomers settle in. You set the price and hours.</p>
                  <div className="mt-4 grid gap-2">
                    <ButtonLink to="/dashboard/profile"><GraduationCap className="h-4 w-4" /> Become a tutor</ButtonLink>
                    <ButtonLink to="/dashboard/profile?kind=GUIDE" variant="secondary"><Compass className="h-4 w-4" /> Become a guide</ButtonLink>
                    <ButtonLink to="/dashboard/profile?kind=MENTOR" variant="secondary"><Briefcase className="h-4 w-4" /> Become a career mentor</ButtonLink>
                  </div>
                </>
              ) : (
                <ul className="mt-3 space-y-3">
                  {profiles.map((p) => (
                    <li key={p.id} className="rounded-lg border border-ink-200 p-3">
                      <div className="flex items-center justify-between">
                        <span className="text-sm font-semibold">{KIND_ROLE[p.kind]} profile</span>
                        <Badge tone={p.status === 'ACTIVE' ? 'green' : p.status === 'SUSPENDED' ? 'red' : 'yellow'}>{p.status.toLowerCase()}</Badge>
                      </div>
                      <p className="mt-1 line-clamp-1 text-xs text-ink-500">{p.headline}</p>
                      <p className="mt-1 text-xs text-ink-500">{plural(p.sessionsCompleted, 'session')} · {p.ratingCount ? `${p.ratingAvg.toFixed(1)} ★` : 'no reviews yet'}</p>
                      <div className="mt-2 flex gap-3 text-xs">
                        <Link to={editPath(p.kind)} className="link">Edit</Link>
                        {p.status === 'ACTIVE' && <Link to={providerPath(p)} className="link">View public page</Link>}
                      </div>
                    </li>
                  ))}
                  {(['TUTOR', 'GUIDE', 'MENTOR'] as const).filter((k) => !profiles.some((p) => p.kind === k)).map((k) => (
                    <li key={k}>
                      <Link to={editPath(k)} className="text-sm link">+ Add a {KIND_ROLE[k].toLowerCase()} profile</Link>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            <JoinedCommunitiesCard />
            <section className="card p-5 text-sm">
              <h2 className="font-semibold">Shortcuts</h2>
              <ul className="mt-2 space-y-1.5">
                <li><Link to="/prep" className="link">Placement prep</Link></li>
                <li><Link to="/resume-reviews" className="link">Resume reviews</Link></li>
                <li><Link to="/feed" className="link">Campus feed</Link></li>
                <li><Link to="/notifications" className="link">Notifications</Link></li>
                <li><Link to="/verify" className="link">Verification status</Link></li>
                <li><Link to="/settings" className="link">Account settings</Link></li>
              </ul>
            </section>
          </aside>
        </div>
      )}
    </Container>
  )
}

const STATUS_FILTERS: { value: string; label: string; statuses: BookingStatus[] | null; upcoming?: boolean }[] = [
  { value: 'upcoming', label: 'Upcoming', statuses: ['REQUESTED', 'ACCEPTED'], upcoming: true },
  { value: 'completed', label: 'Completed', statuses: ['COMPLETED'] },
  { value: 'closed', label: 'Cancelled & declined', statuses: ['CANCELLED', 'DECLINED', 'EXPIRED'] },
  { value: 'all', label: 'All', statuses: null },
]

export function Bookings() {
  useDocumentTitle('Bookings')
  const [params, setParams] = useSearchParams()
  const role = (params.get('as') ?? 'STUDENT') as 'STUDENT' | 'PROVIDER'
  const filter = STATUS_FILTERS.find((f) => f.value === params.get('status')) ?? STATUS_FILTERS[0]
  const page = Number(params.get('page') ?? 1)
  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params)
    p.set(k, v)
    if (k !== 'page') p.delete('page')
    setParams(p, { replace: true })
  }
  const { data, loading, error } = useQuery<{ bookings: { total: number; items: Booking[] } }>(
    `query($as: BookingRole!, $s: [BookingStatus!], $u: Boolean, $p: Int) { bookings(as: $as, statuses: $s, upcoming: $u, page: $p, pageSize: 20) { total items { ${BOOKING_CARD} } } }`,
    { as: role, s: filter.statuses, u: !!filter.upcoming, p: page },
  )
  return (
    <Container>
      <PageHeader title="Bookings" />
      <Tabs value={role} onChange={(v) => set('as', v)} tabs={[{ value: 'STUDENT', label: 'Sessions I booked' }, { value: 'PROVIDER', label: 'Sessions I give' }]} />
      <div className="my-4 flex flex-wrap gap-2">
        {STATUS_FILTERS.map((f) => (
          <button key={f.value} onClick={() => set('status', f.value)} className={cx('rounded-full border px-3 py-1 text-sm font-medium', filter.value === f.value ? 'border-ink-900 bg-ink-900 text-white' : 'border-ink-200 bg-white text-ink-700 hover:border-ink-300')}>
            {f.label}
          </button>
        ))}
      </div>
      <ErrorNote error={error} />
      {loading && !data ? (
        <PageLoader />
      ) : data?.bookings.items.length ? (
        <>
          <div className="card divide-y divide-ink-100 overflow-hidden">{data.bookings.items.map((b) => <BookingRow key={b.id} b={b} />)}</div>
          <Pagination page={page} pageSize={20} total={data.bookings.total} onChange={(p) => set('page', String(p))} />
        </>
      ) : (
        <Empty icon={<CalendarDays className="h-10 w-10" />} title="No bookings here">
          {role === 'STUDENT' ? 'When you request a session it shows up here.' : 'Requests from students appear here once your profile is live.'}
        </Empty>
      )}
    </Container>
  )
}

const BOOKING_DETAIL = `${BOOKING_CARD} contact { name email phone } cancelledBy { id displayName } providerUser { id }
  room { ${ROOM} } mentorNotes { id atSeconds body tone createdAt }
  feedback { scores { criterion score } verdict strengths improvements resources updatedAt }`

type ActionKind = 'ACCEPT' | 'DECLINE' | 'CANCEL' | 'COMPLETE'

export function BookingDetail() {
  const { label } = useCatalog()
  const { id = '' } = useParams()
  const { user } = useAuth()
  const toast = useToast()
  const { data, loading, error, refetch } = useQuery<{ booking: Booking | null }>(`query($id: ID!) { booking(id: $id) { ${BOOKING_DETAIL} } }`, { id }, { pollMs: 20_000 })
  const [action, setAction] = useState<ActionKind | null>(null)
  const [reason, setReason] = useState('')
  const [link, setLink] = useState('')
  const [place, setPlace] = useState('')
  const b = data?.booking
  useDocumentTitle(b ? 'Session details' : '')

  const transition = useAction(async () => {
    await gql(`mutation($i: TransitionInput!) { transitionBooking(input: $i) { id } }`, {
      i: { bookingId: id, action, reason: reason || null, meetingLink: link || null, location: place || null },
    })
    setAction(null)
    setReason('')
    toast({ ACCEPT: 'Session confirmed.', DECLINE: 'Request declined.', CANCEL: 'Session cancelled.', COMPLETE: 'Marked as complete.' }[action!])
    refetch()
  })

  if (loading) return <PageLoader />
  if (error) return <Container><ErrorNote error={error} /></Container>
  if (!b || !user) return <Container><Empty title="Booking not found">It may belong to someone else.</Empty></Container>

  const isProvider = b.myRole === 'PROVIDER'
  const started = new Date(b.startAt).getTime() <= Date.now()
  const open = b.status === 'REQUESTED' || b.status === 'ACCEPTED'
  const other = isProvider ? b.student : { id: b.providerUser?.id ?? '', displayName: b.provider.displayName, avatarUrl: b.provider.avatarUrl }
  const localTz = browserTimeZone()
  const what = sessionTitle(b, label)

  const actionCopy: Record<ActionKind, { title: string; button: string; danger?: boolean }> = {
    ACCEPT: { title: 'Accept this session', button: 'Accept' },
    DECLINE: { title: 'Decline this request', button: 'Decline', danger: true },
    CANCEL: { title: 'Cancel this session', button: 'Cancel session', danger: true },
    COMPLETE: { title: 'Mark as complete', button: 'Mark complete' },
  }

  return (
    <Container className="max-w-4xl">
      <Link to="/bookings" className="text-sm link">← All bookings</Link>
      <div className="mt-4 grid gap-6 md:grid-cols-[1fr_300px]">
        <div className="space-y-6">
          <section className="card p-6">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p className="text-sm text-ink-500">{KIND_SESSION[b.kind]}</p>
                <h1 className="text-xl font-bold">{what}</h1>
              </div>
              <BookingStatusBadge status={b.status} />
            </div>
            {b.statusReason && <p className="mt-3 rounded-lg bg-ink-50 px-3 py-2 text-sm text-ink-700">{b.cancelledBy ? `${b.cancelledBy.displayName}: ` : ''}{b.statusReason}</p>}
            <dl className="mt-5 grid gap-4 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-ink-500">When</dt>
                <dd className="font-medium">{formatDateTime(b.startAt, b.timezone)} – {formatTime(b.endAt, b.timezone)} {zoneLabel(b.timezone)}</dd>
                {formatDateTime(b.startAt, localTz) !== formatDateTime(b.startAt, b.timezone) && <dd className="text-xs text-ink-500">{formatDateTime(b.startAt, localTz)} your time</dd>}
              </div>
              <div>
                <dt className="text-ink-500">Price</dt>
                <dd className="font-medium">{formatMoney(b.price)} <span className="font-normal text-ink-500">· paid directly</span></dd>
              </div>
              <div>
                <dt className="text-ink-500">Format</dt>
                <dd className="flex items-center gap-1 font-medium">{b.mode === 'ONLINE' ? <Video className="h-4 w-4" /> : <MapPin className="h-4 w-4" />} {label('sessionModes', b.mode)}{b.location && ` · ${b.location}`}</dd>
              </div>
              {b.meetingLink && (
                <div>
                  <dt className="text-ink-500">Meeting link</dt>
                  <dd><a href={b.meetingLink} target="_blank" rel="noopener noreferrer" className="link break-all">{b.meetingLink}</a></dd>
                </div>
              )}
            </dl>
            {b.notes && (
              <div className="mt-5">
                <p className="text-sm text-ink-500">Note from {isProvider ? b.student.displayName : 'you'}</p>
                <p className="mt-1 whitespace-pre-line text-sm text-ink-800">{b.notes}</p>
              </div>
            )}
            <div className="mt-6 flex flex-wrap gap-2 border-t border-ink-100 pt-4">
              {isProvider && b.status === 'REQUESTED' && (
                <>
                  <Button onClick={() => { setAction('ACCEPT'); setPlace(b.location) }}>Accept</Button>
                  <Button variant="secondary" onClick={() => setAction('DECLINE')}>Decline</Button>
                </>
              )}
              {isProvider && b.status === 'ACCEPTED' && started && <Button onClick={() => setAction('COMPLETE')}>Mark complete</Button>}
              {open && !started && !(isProvider && b.status === 'REQUESTED') && <Button variant="ghost" className="text-red-600" onClick={() => setAction('CANCEL')}>{b.status === 'REQUESTED' && !isProvider ? 'Withdraw request' : 'Cancel session'}</Button>}
              <div className="ml-auto self-center"><ReportButton targetType="BOOKING" targetId={b.id} label="Report a problem" /></div>
            </div>
          </section>
          {b.mode === 'ONLINE' && <RoomCard booking={b} />}
          {b.kind === 'MENTOR' && <InterviewFeedbackCard booking={b} onSaved={refetch} />}
          {b.status === 'COMPLETED' && !isProvider && !b.reviewed && (
            <StarReview title={`How was your ${b.kind === 'MENTOR' ? 'interview' : 'session'} with ${b.provider.displayName}?`} onDone={refetch}
              submit={(rating, comment) => gql(`mutation($b: ID!, $r: Int!, $c: String) { leaveReview(bookingId: $b, rating: $r, comment: $c) { id } }`, { b: b.id, r: rating, c: comment })} />
          )}
          <MessageThread booking={b} />
        </div>
        <aside className="space-y-4">
          <section className="card p-5">
            <p className="text-xs font-semibold uppercase tracking-wide text-ink-500">{isProvider ? 'Student' : KIND_ROLE[b.kind]}</p>
            <div className="mt-3 flex items-center gap-3">
              <Avatar name={other.displayName} url={other.avatarUrl} size={44} />
              <div>
                <p className="font-semibold">{other.displayName}</p>
                {isProvider ? (
                  <Link to={`/users/${b.student.id}`} className="text-xs link">View profile</Link>
                ) : (
                  <Link to={providerPath(b.provider)} className="text-xs link">View profile</Link>
                )}
              </div>
            </div>
            {b.contact ? (
              <ul className="mt-4 space-y-2 text-sm">
                <li className="flex items-center gap-2"><Mail className="h-4 w-4 text-ink-400" /> <a href={`mailto:${b.contact.email}`} className="link break-all">{b.contact.email}</a></li>
                {b.contact.phone && <li className="flex items-center gap-2"><Phone className="h-4 w-4 text-ink-400" /> <a href={`tel:${b.contact.phone}`} className="link">{b.contact.phone}</a></li>}
              </ul>
            ) : (
              <p className="mt-4 text-xs text-ink-500">Contact details appear once the session is accepted.</p>
            )}
          </section>
          <section className="card p-5 text-xs text-ink-600">
            <p className="font-semibold text-ink-800">Good to know</p>
            <ul className="mt-2 list-disc space-y-1 pl-4">
              <li>Requests expire if not answered before the start time.</li>
              <li>Agree on payment before the session; Your Mate doesn't charge.</li>
              <li>Accepted sessions complete automatically 2 days after they end.</li>
            </ul>
          </section>
        </aside>
      </div>

      <Modal
        open={!!action}
        onClose={() => setAction(null)}
        title={action ? actionCopy[action].title : ''}
        footer={
          <>
            <Button variant="secondary" onClick={() => setAction(null)}>Back</Button>
            {action && <Button variant={actionCopy[action].danger ? 'danger' : 'primary'} loading={transition.pending} onClick={() => transition.run()}>{actionCopy[action].button}</Button>}
          </>
        }
      >
        <div className="space-y-4">
          {action === 'ACCEPT' && (b.mode === 'ONLINE' ? (
            <Field label="Meeting link" hint="Google Meet, Zoom, Teams… You can also share it in the messages later.">
              <Input type="url" value={link} onChange={(e) => setLink(e.target.value)} placeholder="https://meet.google.com/…" />
            </Field>
          ) : (
            <Field label="Meeting place" hint="Confirm or change the spot the student suggested.">
              <Input value={place} onChange={(e) => setPlace(e.target.value)} maxLength={200} />
            </Field>
          ))}
          {(action === 'DECLINE' || action === 'CANCEL') && (
            <Field label={action === 'CANCEL' && b.status === 'ACCEPTED' ? 'Reason (required)' : 'Reason (optional)'} hint="Shared with the other person.">
              <Textarea value={reason} onChange={(e) => setReason(e.target.value)} rows={3} maxLength={500} />
            </Field>
          )}
          {action === 'COMPLETE' && <p className="text-sm text-ink-600">Confirm the session took place. The student will be asked to leave a review.</p>}
          <ErrorNote error={transition.error} />
        </div>
      </Modal>
    </Container>
  )
}

function MessageThread({ booking }: { booking: Booking }) {
  const [body, setBody] = useState('')
  const bottom = useRef<HTMLDivElement>(null)
  const { data, refetch } = useQuery<{ booking: { messages: Message[] } | null }>(
    `query($id: ID!) { booking(id: $id) { messages { id body createdAt mine sender { id displayName avatarUrl } } } }`,
    { id: booking.id },
    { pollMs: 5_000 },
  )
  const messages = data?.booking?.messages ?? []
  useEffect(() => {
    bottom.current?.scrollIntoView({ block: 'nearest' })
  }, [messages.length])
  const send = useAction(async () => {
    await gql(`mutation($b: ID!, $t: String!) { sendMessage(bookingId: $b, body: $t) { id } }`, { b: booking.id, t: body })
    setBody('')
    refetch()
  })
  const closed = !['REQUESTED', 'ACCEPTED', 'COMPLETED'].includes(booking.status)
  return (
    <section className="card flex flex-col">
      <h2 className="border-b border-ink-100 px-5 py-3 font-semibold">Messages</h2>
      <div className="max-h-96 min-h-32 space-y-3 overflow-y-auto px-5 py-4">
        {messages.length === 0 && <p className="py-6 text-center text-sm text-ink-500">No messages yet. Say hello and share what you'd like to cover.</p>}
        {messages.map((m) => (
          <div key={m.id} className={cx('flex', m.mine ? 'justify-end' : 'justify-start')}>
            <div className={cx('max-w-[80%] rounded-2xl px-3.5 py-2 text-sm', m.mine ? 'rounded-br-sm bg-brand-500 text-white' : 'rounded-bl-sm bg-ink-100 text-ink-900')}>
              <p className="whitespace-pre-line break-words">{m.body}</p>
              <p className={cx('mt-0.5 text-[10px]', m.mine ? 'text-brand-100' : 'text-ink-500')}>{timeAgo(m.createdAt)}</p>
            </div>
          </div>
        ))}
        <div ref={bottom} />
      </div>
      {closed ? (
        <p className="border-t border-ink-100 px-5 py-3 text-center text-xs text-ink-500">This conversation is closed.</p>
      ) : (
        <form className="flex gap-2 border-t border-ink-100 p-3" onSubmit={(e) => { e.preventDefault(); if (body.trim()) send.run() }}>
          <Input value={body} onChange={(e) => setBody(e.target.value)} placeholder="Write a message" maxLength={2000} aria-label="Message" />
          <Button type="submit" loading={send.pending} aria-label="Send"><Send className="h-4 w-4" /></Button>
        </form>
      )}
      {send.error ? <ErrorNote error={send.error} className="mx-3 mb-3" /> : null}
    </section>
  )
}

export function Notifications() {
  useDocumentTitle('Notifications')
  const { data, loading, refetch } = useQuery<{ notifications: { unreadCount: number; items: Notification[] } }>(
    `{ notifications(limit: 100) { unreadCount items { id type title body link read createdAt } } }`,
  )
  const markAll = useAction(async () => {
    await gql(`mutation { markNotificationsRead }`)
    refetch()
  })
  return (
    <Container className="max-w-3xl">
      <PageHeader title="Notifications" actions={(data?.notifications.unreadCount ?? 0) > 0 && <Button variant="secondary" loading={markAll.pending} onClick={() => markAll.run()}>Mark all as read</Button>} />
      {loading && !data ? (
        <PageLoader />
      ) : data?.notifications.items.length ? (
        <ul className="card divide-y divide-ink-100 overflow-hidden">
          {data.notifications.items.map((n) => (
            <li key={n.id}>
              <Link
                to={n.link || '#'}
                onClick={() => !n.read && gql(`mutation($ids: [ID!]) { markNotificationsRead(ids: $ids) }`, { ids: [n.id] }).catch(() => {})}
                className={cx('flex gap-3 px-5 py-4 hover:bg-ink-50', !n.read && 'bg-brand-50/50')}
              >
                <span className={cx('mt-1.5 h-2 w-2 shrink-0 rounded-full', n.read ? 'bg-transparent' : 'bg-brand-500')} />
                <div>
                  <p className="text-sm font-semibold">{n.title}</p>
                  <p className="text-sm text-ink-600">{n.body}</p>
                  <p className="mt-1 text-xs text-ink-400">{timeAgo(n.createdAt)}</p>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <Empty icon={<Bell className="h-10 w-10" />} title="No notifications yet">Booking updates, messages and reviews show up here.</Empty>
      )}
    </Container>
  )
}
