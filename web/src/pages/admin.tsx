import { useState, type ReactNode } from 'react'
import { Link, NavLink, Outlet, useSearchParams } from 'react-router-dom'
import { BarChart3, BookOpen, Building2, CalendarDays, ClipboardCheck, ExternalLink, FileCheck2, Flag, Inbox, Newspaper, UserSquare2, Users } from 'lucide-react'
import { Container } from '../components/Layout'
import { BookingStatusBadge, providerPath } from '../components/domain'
import { PostCard } from './feed'
import { Avatar, Badge, Button, Empty, ErrorNote, Field, Input, Modal, PageLoader, Pagination, Select, Textarea, cx } from '../components/ui'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { gql } from '../lib/graphql'
import { useAction, useDebounced, useDocumentTitle, useQuery } from '../lib/hooks'
import { BOOKING_CARD, CAMPUS_ADMIN, POST, PROVIDER_CARD, EXPERIENCE } from '../lib/fragments'
import { enumLabel, plural, formatDate, formatDateTime, formatMoney, timeAgo } from '../lib/format'
import type { Booking, Campus, Page, Post, Provider, User, Verification, InterviewExperience } from '../lib/types'

const SECTIONS = [
  { to: '/admin', label: 'Overview', icon: BarChart3, end: true },
  { to: '/admin/verifications', label: 'ID checks', icon: FileCheck2 },
  { to: '/admin/campuses', label: 'Campuses', icon: Building2 },
  { to: '/admin/users', label: 'Users', icon: Users },
  { to: '/admin/providers', label: 'Providers', icon: UserSquare2 },
  { to: '/admin/posts', label: 'Posts', icon: Newspaper },
  { to: '/admin/experiences', label: 'Experiences', icon: ClipboardCheck },
  { to: '/admin/reports', label: 'Reports', icon: Flag },
  { to: '/admin/bookings', label: 'Bookings', icon: CalendarDays },
  { to: '/admin/inquiries', label: 'Inquiries', icon: Inbox },
  { to: '/admin/subjects', label: 'Subjects', icon: BookOpen },
]

export function AdminLayout() {
  return (
    <Container className="max-w-7xl">
      <div className="grid gap-6 lg:grid-cols-[200px_1fr]">
        <nav className="-mx-4 flex gap-1 overflow-x-auto px-4 lg:mx-0 lg:flex-col lg:px-0">
          <p className="hidden px-3 pb-2 text-xs font-semibold uppercase tracking-wide text-ink-500 lg:block">Admin</p>
          {SECTIONS.map((s) => (
            <NavLink key={s.to} to={s.to} end={s.end} className={({ isActive }) => cx('flex shrink-0 items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium', isActive ? 'bg-brand-50 text-brand-800' : 'text-ink-600 hover:bg-ink-100')}>
              <s.icon className="h-4 w-4" /> {s.label}
            </NavLink>
          ))}
        </nav>
        <div className="min-w-0">
          <Outlet />
        </div>
      </div>
    </Container>
  )
}

function Title({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
      <h1 className="text-2xl font-bold">{children}</h1>
      {actions}
    </div>
  )
}

function Stat({ label, value, to, alert }: { label: string; value: ReactNode; to?: string; alert?: boolean }) {
  const inner = (
    <div className={cx('card p-4 transition', to && 'hover:border-brand-300', alert && 'border-amber-300 bg-amber-50')}>
      <p className="text-xs font-medium text-ink-500">{label}</p>
      <p className="mt-1 text-2xl font-bold">{value}</p>
    </div>
  )
  return to ? <Link to={to}>{inner}</Link> : inner
}

interface AdminStats {
  [k: string]: number | { name: string; bookings: number; tutors: number }[]
  topSubjects: { name: string; bookings: number; tutors: number }[]
}

export function AdminOverview() {
  useDocumentTitle('Admin')
  const { data, loading, error } = useQuery<{ adminStats: AdminStats }>(
    `{ adminStats { users emailVerified campusVerified pendingVerifications campuses pendingCampuses signupsLast7d activeTutors activeGuides activeMentors resumeReviewsDelivered draftProfiles
       bookingsTotal bookingsRequested bookingsAccepted bookingsCompleted bookingsLast7d acceptRate completionRate averageRating reviews
       publishedPosts pendingPosts openReports inquiries topSubjects { name bookings tutors } } }`,
  )
  if (loading) return <PageLoader />
  if (error || !data) return <ErrorNote error={error} />
  const s = data.adminStats as unknown as Record<string, number> & AdminStats
  const pct = (n: number) => `${Math.round(n * 100)}%`
  return (
    <>
      <Title>Overview</Title>
      <h2 className="mb-2 text-sm font-semibold text-ink-600">Needs attention</h2>
      <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="ID checks waiting" value={s.pendingVerifications} to="/admin/verifications" alert={s.pendingVerifications > 0} />
        <Stat label="Campus requests" value={s.pendingCampuses} to="/admin/campuses?status=PENDING" alert={s.pendingCampuses > 0} />
        <Stat label="Posts to review" value={s.pendingPosts} to="/admin/posts" alert={s.pendingPosts > 0} />
        <Stat label="Open reports" value={s.openReports} to="/admin/reports" alert={s.openReports > 0} />
      </div>
      <h2 className="mb-2 text-sm font-semibold text-ink-600">Funnel</h2>
      <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Users" value={s.users} to="/admin/users" />
        <Stat label="Email verified" value={s.emailVerified} />
        <Stat label="Campus verified" value={s.campusVerified} />
        <Stat label="Signups (7 days)" value={s.signupsLast7d} />
      </div>
      <h2 className="mb-2 text-sm font-semibold text-ink-600">Marketplace</h2>
      <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Active tutors" value={s.activeTutors} to="/admin/providers" />
        <Stat label="Active guides" value={s.activeGuides} to="/admin/providers?kind=GUIDE" />
        <Stat label="Active mentors" value={s.activeMentors} to="/admin/providers?kind=MENTOR" />
        <Stat label="Resumes reviewed" value={s.resumeReviewsDelivered} />
        <Stat label="Draft profiles" value={s.draftProfiles} />
        <Stat label="Active campuses" value={s.campuses} to="/admin/campuses" />
        <Stat label="Bookings (7 days)" value={s.bookingsLast7d} to="/admin/bookings" />
        <Stat label="Accept rate" value={pct(s.acceptRate)} />
        <Stat label="Completion rate" value={pct(s.completionRate)} />
        <Stat label="Avg rating" value={s.reviews ? `${s.averageRating.toFixed(2)} ★` : '—'} />
      </div>
      <div className="card overflow-hidden">
        <h2 className="border-b border-ink-100 px-4 py-3 font-semibold">Subjects: demand vs. supply</h2>
        <table className="w-full text-sm">
          <thead className="bg-ink-50 text-left text-xs text-ink-500">
            <tr><th className="px-4 py-2 font-medium">Subject</th><th className="px-4 py-2 font-medium">Bookings</th><th className="px-4 py-2 font-medium">Active tutors</th></tr>
          </thead>
          <tbody className="divide-y divide-ink-100">
            {s.topSubjects.map((t) => (
              <tr key={t.name}>
                <td className="px-4 py-2">{t.name}</td>
                <td className="px-4 py-2">{t.bookings}</td>
                <td className={cx('px-4 py-2', t.bookings > 0 && t.tutors === 0 && 'font-semibold text-red-600')}>{t.tutors}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  )
}

export function AdminVerifications() {
  useDocumentTitle('ID checks')
  const toast = useToast()
  const [status, setStatus] = useState('PENDING')
  const [page, setPage] = useState(1)
  const [rejecting, setRejecting] = useState<Verification | null>(null)
  const [reason, setReason] = useState('')
  const { data, loading, refetch } = useQuery<{ adminVerifications: Page<Verification> }>(
    `query($s: VerificationStatus, $p: Int) { adminVerifications(status: $s, page: $p, pageSize: 20) { total items { id status reason createdAt reviewedAt documentUrl
      user { id firstName lastName displayName email studentIdNo campusEmailVerified campus { name emailDomains } } } } }`,
    { s: status, p: page },
  )
  const review = useAction(async (id: string, approve: boolean, why?: string) => {
    await gql(`mutation($id: ID!, $a: Boolean!, $r: String) { reviewVerification(id: $id, approve: $a, reason: $r) { status } }`, { id, a: approve, r: why ?? null })
    setRejecting(null)
    setReason('')
    toast(approve ? 'Approved.' : 'Rejected; the student was told why.')
    refetch()
  })
  return (
    <>
      <Title actions={<Select className="w-40" value={status} onChange={(e) => { setStatus(e.target.value); setPage(1) }}><option value="PENDING">Pending</option><option value="APPROVED">Approved</option><option value="REJECTED">Rejected</option></Select>}>Student ID checks</Title>
      <ErrorNote error={review.error} className="mb-3" />
      {loading && !data ? <PageLoader /> : data?.adminVerifications.items.length ? (
        <div className="space-y-3">
          {data.adminVerifications.items.map((v) => (
            <div key={v.id} className="card flex flex-col gap-4 p-4 md:flex-row">
              <div className="flex h-40 w-full shrink-0 items-center justify-center overflow-hidden rounded-lg bg-ink-100 md:w-56">
                {v.documentUrl ? (
                  <a href={v.documentUrl} target="_blank" rel="noopener noreferrer" className="block h-full w-full">
                    <object data={v.documentUrl} className="pointer-events-none h-full w-full object-contain" aria-label="Student ID document">
                      <span className="flex h-full items-center justify-center text-sm link">Open document</span>
                    </object>
                  </a>
                ) : (
                  <span className="text-xs text-ink-500">Document deleted after review</span>
                )}
              </div>
              <div className="min-w-0 flex-1 text-sm">
                <p className="font-semibold">{v.user?.firstName} {v.user?.lastName}</p>
                <p className="text-ink-600">{v.user?.email}</p>
                <p className="text-ink-600">{v.user?.campus?.name}</p>
                <p className="mt-1 text-ink-600">Student ID on profile: <span className="font-mono">{v.user?.studentIdNo || '—'}</span></p>
                <p className="mt-1 text-xs text-ink-500">Submitted {v.createdAt && timeAgo(v.createdAt)}{v.reviewedAt && ` · reviewed ${formatDate(v.reviewedAt)}`}</p>
                {v.reason && <p className="mt-1 text-xs text-red-700">Reason: {v.reason}</p>}
                {v.documentUrl && <a href={v.documentUrl} target="_blank" rel="noopener noreferrer" className="mt-2 inline-flex items-center gap-1 text-xs link">Open full size <ExternalLink className="h-3 w-3" /></a>}
              </div>
              {v.status === 'PENDING' && (
                <div className="flex shrink-0 gap-2 md:flex-col">
                  <Button onClick={() => review.run(v.id!, true)} loading={review.pending}>Approve</Button>
                  <Button variant="secondary" onClick={() => setRejecting(v)}>Reject</Button>
                </div>
              )}
            </div>
          ))}
          <Pagination page={page} pageSize={20} total={data.adminVerifications.total} onChange={setPage} />
        </div>
      ) : <Empty icon={<FileCheck2 className="h-10 w-10" />} title="Queue is empty" />}
      <Modal open={!!rejecting} onClose={() => setRejecting(null)} title="Reject ID" footer={<><Button variant="secondary" onClick={() => setRejecting(null)}>Cancel</Button><Button variant="danger" disabled={!reason.trim()} loading={review.pending} onClick={() => review.run(rejecting!.id!, false, reason)}>Reject</Button></>}>
        <Field label="Reason shown to the student" hint="Be specific so they can fix it, e.g. “The photo is blurry” or “The card expired in 2024”.">
          <Textarea value={reason} onChange={(e) => setReason(e.target.value)} rows={3} />
        </Field>
      </Modal>
    </>
  )
}

export function AdminCampuses() {
  useDocumentTitle('Campuses')
  const toast = useToast()
  const [params, setParams] = useSearchParams()
  const status = params.get('status') ?? ''
  const country = params.get('country') ?? ''
  const needsDetails = params.get('needs') === '1'
  const page = Number(params.get('page') ?? 1)
  const [q, setQ] = useState('')
  const [editing, setEditing] = useState<Campus | null>(null)
  const query = useDebounced(q)
  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params)
    if (v) p.set(k, v)
    else p.delete(k)
    if (k !== 'page') p.delete('page')
    setParams(p, { replace: true })
  }
  const { data: counts } = useQuery<{ campusCountries: { country: string; campuses: number }[] }>(`{ campusCountries { country campuses } }`)
  const { data, loading, refetch } = useQuery<{ adminCampuses: Page<Campus> }>(
    `query($s: CampusStatus, $q: String, $c: String, $n: Boolean, $p: Int) {
      adminCampuses(status: $s, query: $q, country: $c, needsDetails: $n, page: $p, pageSize: 50) { total items { ${CAMPUS_ADMIN} } } }`,
    { s: status || null, q: query || null, c: country || null, n: needsDetails, p: page },
  )
  const review = useAction(async (id: string, approve: boolean) => {
    await gql(`mutation($id: ID!, $a: Boolean!) { reviewCampus(id: $id, approve: $a) { status } }`, { id, a: approve })
    toast(approve ? 'Campus is live.' : 'Campus rejected.')
    refetch()
  })
  return (
    <>
      <Title>Campuses {data && <span className="text-base font-normal text-ink-500">({data.adminCampuses.total})</span>}</Title>
      <div className="mb-4 flex flex-wrap gap-2">
        <Input placeholder="Search name, initials, domain" value={q} onChange={(e) => { setQ(e.target.value); set('page', '') }} className="w-60" />
        <Select className="w-36" value={status} onChange={(e) => set('status', e.target.value)} aria-label="Status">
          <option value="">All statuses</option><option value="PENDING">Pending</option><option value="ACTIVE">Active</option><option value="REJECTED">Rejected</option>
        </Select>
        <Select className="w-44" value={country} onChange={(e) => set('country', e.target.value)} aria-label="Country">
          <option value="">All countries</option>
          {counts?.campusCountries.map((c) => <option key={c.country} value={c.country}>{c.country} ({c.campuses})</option>)}
        </Select>
        <label className="flex items-center gap-2 rounded-lg border border-ink-200 bg-white px-3 text-sm">
          <input type="checkbox" checked={needsDetails} onChange={(e) => set('needs', e.target.checked ? '1' : '')} /> Details not verified
        </label>
      </div>
      <ErrorNote error={review.error} className="mb-3" />
      {loading && !data ? <PageLoader /> : (
        <>
          <div className="card overflow-x-auto">
            <table className="w-full min-w-[820px] text-sm">
              <thead className="bg-ink-50 text-left text-xs text-ink-500">
                <tr><th className="px-4 py-2 font-medium">Campus</th><th className="px-4 py-2 font-medium">Email domains</th><th className="px-4 py-2 font-medium">Zone / currency</th><th className="px-4 py-2 font-medium">Members</th><th className="px-4 py-2 font-medium">Status</th><th /></tr>
              </thead>
              <tbody className="divide-y divide-ink-100">
                {data?.adminCampuses.items.map((c) => (
                  <tr key={c.id}>
                    <td className="px-4 py-2.5">
                      <p className="font-medium">{c.name}</p>
                      <p className="text-xs text-ink-500">
                        {[c.city, c.country].filter(Boolean).join(', ')} · {c.source?.toLowerCase()}
                        {c.website && <> · <a href={c.website} target="_blank" rel="noopener noreferrer" className="link">website</a></>}
                      </p>
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs">{c.emailDomains.join(', ') || <span className="font-sans text-ink-400">none — ID check only</span>}</td>
                    <td className="px-4 py-2.5 text-xs">{c.timezone}<br />{c.currency}</td>
                    <td className="px-4 py-2.5">{c.memberCount}</td>
                    <td className="space-y-1 px-4 py-2.5">
                      <Badge tone={c.status === 'ACTIVE' ? 'green' : c.status === 'PENDING' ? 'yellow' : 'red'}>{c.status.toLowerCase()}</Badge>
                      {!c.detailsVerified && <div><Badge tone="gray" className="whitespace-nowrap">unverified</Badge></div>}
                    </td>
                    <td className="whitespace-nowrap px-4 py-2.5 text-right">
                      <Button size="sm" variant="ghost" onClick={() => setEditing(c)}>Edit</Button>
                      {c.status !== 'ACTIVE' && <Button size="sm" onClick={() => review.run(c.id, true)}>Approve</Button>}
                      {c.status === 'PENDING' && <Button size="sm" variant="ghost" onClick={() => review.run(c.id, false)}>Reject</Button>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination page={page} pageSize={50} total={data?.adminCampuses.total ?? 0} onChange={(p) => set('page', String(p))} />
        </>
      )}
      <p className="mt-3 text-xs text-ink-500">
        Check each email domain really belongs to the institution before saving it: members who verify an email on it become campus-verified automatically, including people who already did.
      </p>
      {editing && <CampusEditor campus={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); toast('Campus saved.'); refetch() }} />}
    </>
  )
}

function CampusEditor({ campus, onClose, onSaved }: { campus: Campus; onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState({
    name: campus.name,
    country: campus.country,
    city: campus.city,
    timezone: campus.timezone,
    currency: campus.currency,
    website: campus.website ?? '',
    domains: campus.emailDomains.join(', '),
    detailsVerified: !!campus.detailsVerified,
  })
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value })
  const save = useAction(async () => {
    await gql(`mutation($id: ID!, $i: UpdateCampusInput!) { updateCampus(id: $id, input: $i) { id } }`, {
      id: campus.id,
      i: { ...form, domains: undefined, emailDomains: form.domains.split(/[\s,]+/).filter(Boolean) },
    })
    onSaved()
  })
  return (
    <Modal open onClose={onClose} title="Edit campus" footer={<><Button variant="secondary" onClick={onClose}>Cancel</Button><Button loading={save.pending} onClick={() => save.run()}>Save</Button></>}>
      <div className="space-y-4">
        <Field label="Name"><Input value={form.name} onChange={set('name')} /></Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Country"><Input value={form.country} onChange={set('country')} /></Field>
          <Field label="City"><Input value={form.city} onChange={set('city')} /></Field>
          <Field label="Timezone" hint="IANA name, e.g. Asia/Kolkata"><Input value={form.timezone} onChange={set('timezone')} /></Field>
          <Field label="Currency" hint={campus.memberCount ? 'Locked: the campus has members.' : 'ISO code, e.g. INR'}>
            <Input value={form.currency} disabled={!!campus.memberCount} onChange={set('currency')} maxLength={3} />
          </Field>
        </div>
        <Field label="Official website"><Input type="url" value={form.website} onChange={set('website')} placeholder="https://" /></Field>
        <Field label="Student email domains" hint="Comma-separated, e.g. iitb.ac.in. Leave empty if unsure — students then verify with their ID.">
          <Input value={form.domains} onChange={set('domains')} className="font-mono" />
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={form.detailsVerified} onChange={(e) => setForm({ ...form, detailsVerified: e.target.checked })} />
          I've checked these details against the institution's official sources
        </label>
        <ErrorNote error={save.error} />
      </div>
    </Modal>
  )
}

export function AdminUsers() {
  useDocumentTitle('Users')
  const { user: me } = useAuth()
  const toast = useToast()
  const [params] = useSearchParams()
  const [q, setQ] = useState(params.get('q') ?? '')
  const [page, setPage] = useState(1)
  const query = useDebounced(q)
  const { data, loading, refetch } = useQuery<{ adminUsers: Page<User> }>(
    `query($q: String, $p: Int) { adminUsers(query: $q, page: $p, pageSize: 25) { total items { id firstName lastName displayName avatarUrl email emailVerified campusVerified idVerified isAdmin status joinedAt campus { name } } } }`,
    { q: query, p: page },
  )
  const act = useAction(async (op: 'status' | 'admin', u: User) => {
    if (op === 'status') await gql(`mutation($u: ID!, $s: UserStatus!) { setUserStatus(userId: $u, status: $s) { id } }`, { u: u.id, s: u.status === 'SUSPENDED' ? 'ACTIVE' : 'SUSPENDED' })
    else await gql(`mutation($u: ID!, $a: Boolean!) { setUserAdmin(userId: $u, admin: $a) { id } }`, { u: u.id, a: !u.isAdmin })
    toast('Updated.')
    refetch()
  })
  return (
    <>
      <Title actions={<Input placeholder="Name, email or student ID" value={q} onChange={(e) => { setQ(e.target.value); setPage(1) }} className="w-64" />}>Users {data && <span className="text-base font-normal text-ink-500">({data.adminUsers.total})</span>}</Title>
      <ErrorNote error={act.error} className="mb-3" />
      {loading && !data ? <PageLoader /> : (
        <>
          <div className="card overflow-x-auto">
            <table className="w-full min-w-[760px] text-sm">
              <thead className="bg-ink-50 text-left text-xs text-ink-500">
                <tr><th className="px-4 py-2 font-medium">User</th><th className="px-4 py-2 font-medium">Campus</th><th className="px-4 py-2 font-medium">Verification</th><th className="px-4 py-2 font-medium">Joined</th><th /></tr>
              </thead>
              <tbody className="divide-y divide-ink-100">
                {data?.adminUsers.items.map((u) => (
                  <tr key={u.id} className={cx(u.status === 'SUSPENDED' && 'bg-red-50/50')}>
                    <td className="px-4 py-2.5">
                      <div className="flex items-center gap-2">
                        <Avatar name={u.displayName} url={u.avatarUrl} size={30} />
                        <div><p className="font-medium">{u.firstName} {u.lastName} {u.isAdmin && <Badge tone="blue">admin</Badge>} {u.status === 'SUSPENDED' && <Badge tone="red">suspended</Badge>}</p><p className="text-xs text-ink-500">{u.email}</p></div>
                      </div>
                    </td>
                    <td className="px-4 py-2.5 text-xs">{u.campus?.name}</td>
                    <td className="space-x-1 px-4 py-2.5">
                      {u.emailVerified ? <Badge>email</Badge> : <Badge tone="yellow">unconfirmed</Badge>}
                      {u.campusVerified && <Badge tone="green">campus</Badge>}
                      {u.idVerified && <Badge tone="brand">ID</Badge>}
                    </td>
                    <td className="px-4 py-2.5 text-xs">{formatDate(u.joinedAt)}</td>
                    <td className="whitespace-nowrap px-4 py-2.5 text-right">
                      {u.id !== me?.id && (
                        <>
                          <Button size="sm" variant="ghost" onClick={() => act.run('admin', u)}>{u.isAdmin ? 'Remove admin' : 'Make admin'}</Button>
                          <Button size="sm" variant={u.status === 'SUSPENDED' ? 'secondary' : 'ghost'} className={u.status === 'SUSPENDED' ? '' : 'text-red-600'} onClick={() => act.run('status', u)}>{u.status === 'SUSPENDED' ? 'Reinstate' : 'Suspend'}</Button>
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination page={page} pageSize={25} total={data?.adminUsers.total ?? 0} onChange={setPage} />
        </>
      )}
    </>
  )
}

export function AdminProviders() {
  useDocumentTitle('Providers')
  const toast = useToast()
  const [params, setParams] = useSearchParams()
  const kind = (['GUIDE', 'MENTOR'] as const).find((k) => k === params.get('kind')) ?? 'TUTOR'
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const query = useDebounced(q)
  const { data, loading, refetch } = useQuery<{ adminProviders: Page<Provider> }>(
    `query($k: ProviderKind!, $q: String, $p: Int) { adminProviders(kind: $k, query: $q, page: $p, pageSize: 25) { total items { ${PROVIDER_CARD} user { id displayName } } } }`,
    { k: kind, q: query, p: page },
  )
  const setStatus = useAction(async (id: string, status: string) => {
    await gql(`mutation($id: ID!, $s: ProviderStatus!) { setProviderStatus(id: $id, status: $s) { status } }`, { id, s: status })
    toast(status === 'SUSPENDED' ? 'Suspended; the provider was notified.' : 'Restored.')
    refetch()
  })
  return (
    <>
      <Title actions={
        <div className="flex gap-2">
          <Input placeholder="Search" value={q} onChange={(e) => setQ(e.target.value)} className="w-48" />
          <Select className="w-32" value={kind} onChange={(e) => setParams({ kind: e.target.value })}><option value="TUTOR">Tutors</option><option value="GUIDE">Guides</option><option value="MENTOR">Mentors</option></Select>
        </div>
      }>Providers</Title>
      <ErrorNote error={setStatus.error} className="mb-3" />
      {loading && !data ? <PageLoader /> : (
        <>
          <div className="card divide-y divide-ink-100">
            {data?.adminProviders.items.map((p) => (
              <div key={p.id} className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center">
                <Avatar name={p.displayName} url={p.avatarUrl} size={36} />
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium">{p.displayName} <span className="text-ink-500">· {p.campus?.name}</span></p>
                  <p className="truncate text-xs text-ink-500">{p.headline}</p>
                </div>
                <span className="text-xs text-ink-600">{formatMoney(p.rate)}/h · {p.ratingCount ? `${p.ratingAvg.toFixed(1)}★ (${p.ratingCount})` : 'no reviews'} · {plural(p.sessionsCompleted, 'session')}</span>
                <Badge tone={p.status === 'ACTIVE' ? 'green' : p.status === 'SUSPENDED' ? 'red' : 'yellow'}>{p.status.toLowerCase()}</Badge>
                <div className="flex gap-1">
                  <Link to={providerPath(p)} className="rounded-lg px-2 py-1 text-xs link">View</Link>
                  {p.status === 'SUSPENDED' ? (
                    <Button size="sm" variant="secondary" onClick={() => setStatus.run(p.id, 'ACTIVE')}>Restore</Button>
                  ) : (
                    <Button size="sm" variant="ghost" className="text-red-600" onClick={() => setStatus.run(p.id, 'SUSPENDED')}>Suspend</Button>
                  )}
                </div>
              </div>
            ))}
            {data?.adminProviders.items.length === 0 && <p className="px-4 py-8 text-center text-sm text-ink-500">None yet.</p>}
          </div>
          <Pagination page={page} pageSize={25} total={data?.adminProviders.total ?? 0} onChange={setPage} />
        </>
      )}
    </>
  )
}

export function AdminPosts() {
  useDocumentTitle('Posts')
  const toast = useToast()
  const [status, setStatus] = useState('PENDING')
  const { data, loading, refetch } = useQuery<{ adminPosts: Page<Post> }>(`query($s: PostStatus) { adminPosts(status: $s, pageSize: 50) { total items { ${POST} } } }`, { s: status })
  const review = useAction(async (id: string, approve: boolean) => {
    const reason = approve ? null : window.prompt('Reason for the author (optional)') ?? ''
    await gql(`mutation($id: ID!, $a: Boolean!, $r: String) { reviewPost(id: $id, approve: $a, reason: $r) { status } }`, { id, a: approve, r: reason })
    toast(approve ? 'Published.' : 'Rejected.')
    refetch()
  })
  const remove = useAction(async (id: string) => {
    await gql(`mutation($id: ID!) { deletePost(id: $id) }`, { id })
    refetch()
  })
  return (
    <>
      <Title actions={<Select className="w-40" value={status} onChange={(e) => setStatus(e.target.value)}><option value="PENDING">Pending</option><option value="PUBLISHED">Published</option><option value="REJECTED">Rejected</option></Select>}>Feed posts</Title>
      <ErrorNote error={review.error || remove.error} className="mb-3" />
      {loading && !data ? <PageLoader /> : data?.adminPosts.items.length ? (
        <div className="grid gap-4 xl:grid-cols-2">
          {data.adminPosts.items.map((p) => (
            <div key={p.id} className="space-y-2">
              <PostCard p={p} />
              <div className="flex gap-2">
                {p.status !== 'PUBLISHED' && <Button size="sm" onClick={() => review.run(p.id, true)}>Publish</Button>}
                {p.status === 'PENDING' && <Button size="sm" variant="secondary" onClick={() => review.run(p.id, false)}>Reject</Button>}
                <Button size="sm" variant="ghost" className="text-red-600" onClick={() => remove.run(p.id)}>Delete</Button>
              </div>
            </div>
          ))}
        </div>
      ) : <Empty icon={<Newspaper className="h-10 w-10" />} title="Nothing to review" />}
    </>
  )
}

interface Report {
  id: string
  targetType: string
  targetId: string
  targetLabel: string
  targetLink: string | null
  reason: string
  details: string
  status: string
  resolutionNote: string
  createdAt: string
  reporter: Pick<User, 'id' | 'displayName'>
}

export function AdminReports() {
  useDocumentTitle('Reports')
  const toast = useToast()
  const [status, setStatus] = useState('OPEN')
  const [page, setPage] = useState(1)
  const { data, loading, refetch } = useQuery<{ adminReports: Page<Report> }>(
    `query($s: ReportStatus, $p: Int) { adminReports(status: $s, page: $p, pageSize: 25) { total items { id targetType targetId targetLabel targetLink reason details status resolutionNote createdAt reporter { id displayName } } } }`,
    { s: status, p: page },
  )
  const resolve = useAction(async (id: string, s: 'RESOLVED' | 'DISMISSED') => {
    const note = window.prompt(s === 'RESOLVED' ? 'What action did you take?' : 'Why dismiss? (optional)') ?? ''
    await gql(`mutation($id: ID!, $s: ReportStatus!, $n: String) { resolveReport(id: $id, status: $s, note: $n) { status } }`, { id, s, n: note })
    toast('Report closed.')
    refetch()
  })
  return (
    <>
      <Title actions={<Select className="w-40" value={status} onChange={(e) => { setStatus(e.target.value); setPage(1) }}><option value="OPEN">Open</option><option value="RESOLVED">Resolved</option><option value="DISMISSED">Dismissed</option></Select>}>Reports</Title>
      <ErrorNote error={resolve.error} className="mb-3" />
      {loading && !data ? <PageLoader /> : data?.adminReports.items.length ? (
        <div className="space-y-3">
          {data.adminReports.items.map((r) => (
            <div key={r.id} className="card p-4 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone="red">{enumLabel(r.reason)}</Badge>
                <Badge>{r.targetType.toLowerCase()}</Badge>
                <span className="text-xs text-ink-500">by {r.reporter.displayName} · {timeAgo(r.createdAt)}</span>
              </div>
              <p className="mt-2 font-medium">
                {r.targetLink ? <Link to={r.targetLink} className="link">{r.targetLabel}</Link> : r.targetLabel}
              </p>
              {r.details && <p className="mt-1 whitespace-pre-line text-ink-700">{r.details}</p>}
              {r.resolutionNote && <p className="mt-2 text-xs text-ink-500">Note: {r.resolutionNote}</p>}
              {r.status === 'OPEN' && (
                <div className="mt-3 flex gap-2">
                  <Button size="sm" onClick={() => resolve.run(r.id, 'RESOLVED')}>Mark resolved</Button>
                  <Button size="sm" variant="secondary" onClick={() => resolve.run(r.id, 'DISMISSED')}>Dismiss</Button>
                </div>
              )}
            </div>
          ))}
          <Pagination page={page} pageSize={25} total={data.adminReports.total} onChange={setPage} />
          <p className="text-xs text-ink-500">To act on a report: suspend the user under Users, suspend the profile under Providers, hide a review from the profile page, or delete a post under Posts.</p>
        </div>
      ) : <Empty icon={<Flag className="h-10 w-10" />} title="No reports" />}
    </>
  )
}

export function AdminBookings() {
  useDocumentTitle('Bookings')
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const { data, loading } = useQuery<{ adminBookings: Page<Booking> }>(
    `query($s: [BookingStatus!], $p: Int) { adminBookings(statuses: $s, page: $p, pageSize: 25) { total items { ${BOOKING_CARD} } } }`,
    { s: status ? [status] : null, p: page },
  )
  return (
    <>
      <Title actions={<Select className="w-40" value={status} onChange={(e) => { setStatus(e.target.value); setPage(1) }}><option value="">All</option>{['REQUESTED', 'ACCEPTED', 'COMPLETED', 'CANCELLED', 'DECLINED', 'EXPIRED'].map((s) => <option key={s} value={s}>{enumLabel(s)}</option>)}</Select>}>Bookings</Title>
      {loading && !data ? <PageLoader /> : (
        <>
          <div className="card overflow-x-auto">
            <table className="w-full min-w-[720px] text-sm">
              <thead className="bg-ink-50 text-left text-xs text-ink-500">
                <tr><th className="px-4 py-2 font-medium">When</th><th className="px-4 py-2 font-medium">Student → Provider</th><th className="px-4 py-2 font-medium">What</th><th className="px-4 py-2 font-medium">Price</th><th className="px-4 py-2 font-medium">Status</th></tr>
              </thead>
              <tbody className="divide-y divide-ink-100">
                {data?.adminBookings.items.map((b) => (
                  <tr key={b.id} className="hover:bg-ink-50">
                    <td className="px-4 py-2.5 text-xs"><Link to={`/bookings/${b.id}`} className="link">{formatDateTime(b.startAt, b.timezone)}</Link></td>
                    <td className="px-4 py-2.5">{b.student.displayName} → {b.provider.displayName}</td>
                    <td className="px-4 py-2.5 text-xs">{b.subject?.name ?? b.service ?? b.kind.toLowerCase()}</td>
                    <td className="px-4 py-2.5">{formatMoney(b.price)}</td>
                    <td className="px-4 py-2.5"><BookingStatusBadge status={b.status} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination page={page} pageSize={25} total={data?.adminBookings.total ?? 0} onChange={setPage} />
        </>
      )}
    </>
  )
}

export function AdminInquiries() {
  useDocumentTitle('Inquiries')
  const [page, setPage] = useState(1)
  const { data, loading } = useQuery<{ adminInquiries: Page<{ id: string; name: string; email: string; message: string; createdAt: string }> }>(
    `query($p: Int) { adminInquiries(page: $p, pageSize: 25) { total items { id name email message createdAt } } }`,
    { p: page },
  )
  return (
    <>
      <Title>Contact inquiries</Title>
      {loading && !data ? <PageLoader /> : data?.adminInquiries.items.length ? (
        <div className="space-y-3">
          {data.adminInquiries.items.map((i) => (
            <div key={i.id} className="card p-4 text-sm">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="font-semibold">{i.name} <a href={`mailto:${i.email}`} className="font-normal link">{i.email}</a></p>
                <span className="text-xs text-ink-500">{timeAgo(i.createdAt)}</span>
              </div>
              <p className="mt-2 whitespace-pre-line text-ink-700">{i.message}</p>
            </div>
          ))}
          <Pagination page={page} pageSize={25} total={data.adminInquiries.total} onChange={setPage} />
        </div>
      ) : <Empty icon={<Inbox className="h-10 w-10" />} title="No messages yet" />}
    </>
  )
}

export function AdminSubjects() {
  useDocumentTitle('Subjects')
  const toast = useToast()
  const [form, setForm] = useState({ name: '', code: '', category: '' })
  const { data, refetch } = useQuery<{ subjects: { id: string; name: string; code: string; category: string; campusId: string | null }[] }>(`{ subjects { id name code category campusId } }`)
  const add = useAction(async () => {
    await gql(`mutation($i: SubjectInput!) { createGlobalSubject(input: $i) { id } }`, { i: form })
    setForm({ name: '', code: '', category: '' })
    toast('Subject added for every campus.')
    refetch()
  })
  const categories = [...new Set(data?.subjects.map((s) => s.category))]
  return (
    <>
      <Title>Global subjects</Title>
      <form className="card mb-6 grid gap-3 p-4 sm:grid-cols-[1fr_140px_200px_auto]" onSubmit={(e) => { e.preventDefault(); add.run() }}>
        <Input placeholder="Subject name" required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        <Input placeholder="Code" value={form.code} onChange={(e) => setForm({ ...form, code: e.target.value })} />
        <Input placeholder="Category" list="cats" value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })} />
        <datalist id="cats">{categories.map((c) => <option key={c} value={c} />)}</datalist>
        <Button type="submit" loading={add.pending}>Add</Button>
        <ErrorNote error={add.error} className="sm:col-span-4" />
      </form>
      <div className="space-y-4">
        {categories.map((c) => (
          <div key={c}>
            <p className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-ink-500">{c}</p>
            <div className="flex flex-wrap gap-1.5">{data?.subjects.filter((s) => s.category === c).map((s) => <span key={s.id} className="chip">{s.name}</span>)}</div>
          </div>
        ))}
      </div>
      <p className="mt-6 text-xs text-ink-500">Campus-specific course codes are added by ID-verified tutors from their profile editor.</p>
    </>
  )
}

export function AdminExperiences() {
  useDocumentTitle('Interview experiences')
  const toast = useToast()
  const [status, setStatus] = useState('PENDING')
  const { data, loading, refetch } = useQuery<{ adminExperiences: Page<InterviewExperience> }>(
    `query($s: ExperienceStatus) { adminExperiences(status: $s, pageSize: 50) { total items { ${EXPERIENCE} } } }`, { s: status })
  const review = useAction(async (id: string, approve: boolean) => {
    const reason = approve ? null : window.prompt('Reason for the author (optional)') ?? ''
    await gql(`mutation($id: ID!, $a: Boolean!, $r: String) { reviewInterviewExperience(id: $id, approve: $a, reason: $r) { status } }`, { id, a: approve, r: reason })
    toast(approve ? 'Published.' : 'Rejected.')
    refetch()
  })
  const remove = useAction(async (id: string) => {
    await gql(`mutation($id: ID!) { deleteInterviewExperience(id: $id) }`, { id })
    refetch()
  })
  return (
    <>
      <Title actions={<Select className="w-40" value={status} onChange={(e) => setStatus(e.target.value)}><option value="PENDING">Pending</option><option value="PUBLISHED">Published</option><option value="REJECTED">Rejected</option></Select>}>Interview experiences</Title>
      <p className="mb-3 text-sm text-ink-600">Check that each write-up is a real account of the process, with no leaked confidential tests or personal details.</p>
      <ErrorNote error={review.error || remove.error} className="mb-3" />
      {loading && !data ? <PageLoader /> : data?.adminExperiences.items.length ? (
        <div className="space-y-4">
          {data.adminExperiences.items.map((e) => (
            <article key={e.id} className="card space-y-3 p-5">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <p className="font-semibold">{e.company} · {e.role} · {e.year} <span className="font-normal text-ink-500">({e.outcome.toLowerCase()})</span></p>
                <span className="text-xs text-ink-500">{e.author?.displayName ?? 'Unknown'}{e.anonymous ? ' · posting anonymously' : ''}{e.campus ? ` · ${e.campus.name}` : ''}</span>
              </div>
              <ol className="list-decimal space-y-1 pl-5 text-sm">{e.rounds.map((r, i) => <li key={i}><b>{r.name}.</b> <span className="whitespace-pre-line text-ink-700">{r.details}</span></li>)}</ol>
              {e.tips && <p className="text-sm text-ink-700"><b>Tips:</b> {e.tips}</p>}
              <div className="flex gap-2">
                {e.status !== 'PUBLISHED' && <Button size="sm" onClick={() => review.run(e.id, true)}>Publish</Button>}
                {e.status === 'PENDING' && <Button size="sm" variant="secondary" onClick={() => review.run(e.id, false)}>Reject</Button>}
                <Button size="sm" variant="ghost" className="text-red-600" onClick={() => remove.run(e.id)}>Delete</Button>
              </div>
            </article>
          ))}
        </div>
      ) : <Empty icon={<ClipboardCheck className="h-10 w-10" />} title="Nothing to review" />}
    </>
  )
}
