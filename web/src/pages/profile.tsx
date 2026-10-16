import type { ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { BadgeCheck, BookOpen, CalendarCheck, Circle, Compass, GraduationCap, Mail, MapPin, Pencil, School, Shield, Star } from 'lucide-react'
import { Container } from '../components/Layout'
import { ProviderCard, ReportButton, providerPath } from '../components/domain'
import { Avatar, Badge, Button, ButtonLink, Empty, ErrorNote, PageLoader, cx, type Tone } from '../components/ui'
import { useToast } from '../components/toast'
import { MessageButton } from './messages'
import { useAuth } from '../lib/auth'
import { gql } from '../lib/graphql'
import { useAction, useDocumentTitle, useQuery } from '../lib/hooks'
import { PROVIDER_CARD } from '../lib/fragments'
import { formatDate, plural } from '../lib/format'
import type { Provider, ProviderKind, ProviderStatus, User, Verification } from '../lib/types'

const statusTone: Record<ProviderStatus, Tone> = { ACTIVE: 'green', DRAFT: 'yellow', PAUSED: 'gray', SUSPENDED: 'red' }
const statusLabel: Record<ProviderStatus, string> = { ACTIVE: 'Live', DRAFT: 'Draft', PAUSED: 'Paused', SUSPENDED: 'Suspended' }

function Check({ done, label, detail, action }: { done: boolean; label: string; detail: ReactNode; action?: ReactNode }) {
  return (
    <li className="flex items-start gap-3 py-3">
      {done ? <BadgeCheck className="mt-0.5 h-5 w-5 shrink-0 text-emerald-500" /> : <Circle className="mt-0.5 h-5 w-5 shrink-0 text-ink-300" />}
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-ink-900">{label}</p>
        <p className="text-sm text-ink-600">{detail}</p>
      </div>
      {action}
    </li>
  )
}

/** One of the three ways to use Your Mate, with what it takes to unlock it. */
function RoleCard({ icon, title, status, children, action }: { icon: ReactNode; title: string; status: ReactNode; children: ReactNode; action?: ReactNode }) {
  return (
    <div className="card flex flex-col p-5">
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-2 font-semibold">
          <span className="rounded-lg bg-brand-100 p-1.5 text-brand-600">{icon}</span> {title}
        </span>
        {status}
      </div>
      <div className="mt-3 flex-1 text-sm text-ink-600">{children}</div>
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

interface MyProfileData {
  myProviderProfiles: Pick<Provider, 'id' | 'kind' | 'status' | 'headline' | 'ratingAvg' | 'ratingCount' | 'sessionsCompleted'>[]
  myVerification: Verification
  booked: { total: number }
  given: { total: number }
  upcoming: { total: number }
}

export function MyProfile() {
  useDocumentTitle('My profile')
  const { user } = useAuth()
  const { data, loading, error } = useQuery<MyProfileData>(`{
    myProviderProfiles { id kind status headline ratingAvg ratingCount sessionsCompleted }
    myVerification { status reason }
    booked: bookings(as: STUDENT, statuses: [COMPLETED], pageSize: 1) { total }
    given: bookings(as: PROVIDER, statuses: [COMPLETED], pageSize: 1) { total }
    upcoming: bookings(as: ANY, statuses: [REQUESTED, ACCEPTED], upcoming: true, pageSize: 1) { total }
  }`)
  if (!user) return null
  if (loading && !data) return <PageLoader />
  const profile = (k: ProviderKind) => data?.myProviderProfiles.find((p) => p.kind === k)
  const idPending = data?.myVerification.status === 'PENDING'
  const rated = data?.myProviderProfiles.filter((p) => p.ratingCount > 0) ?? []

  const providerRole = (kind: ProviderKind) => {
    const p = profile(kind)
    const isTutor = kind === 'TUTOR'
    const editPath = `/dashboard/profile${isTutor ? '' : '?kind=GUIDE'}`
    return (
      <RoleCard
        icon={isTutor ? <GraduationCap className="h-4 w-4" /> : <Compass className="h-4 w-4" />}
        title={isTutor ? 'Tutor' : 'Campus guide'}
        status={p ? <Badge tone={statusTone[p.status]}>{statusLabel[p.status]}</Badge> : <Badge>Not set up</Badge>}
        action={
          p ? (
            <div className="flex gap-2">
              <ButtonLink to={editPath} variant="secondary" size="sm">Edit</ButtonLink>
              {p.status === 'ACTIVE' && <ButtonLink to={providerPath(p)} variant="ghost" size="sm">Public page</ButtonLink>}
            </div>
          ) : (
            <ButtonLink to={editPath} size="sm">{isTutor ? 'Become a tutor' : 'Become a guide'}</ButtonLink>
          )
        }
      >
        {p?.status === 'ACTIVE' ? (
          <>Students can find and book you. {plural(p.sessionsCompleted, 'session')} given{p.ratingCount ? ` · ${p.ratingAvg.toFixed(1)} ★ (${p.ratingCount})` : ''}.</>
        ) : p?.status === 'SUSPENDED' ? (
          <>An admin suspended this profile. Contact us to appeal.</>
        ) : !user.idVerified ? (
          <>Needs a <strong>verified student ID</strong> before it can go live{idPending ? ' — yours is being reviewed.' : '.'}{p ? ' Your draft is saved.' : ''}</>
        ) : p ? (
          <>Your ID is verified. Publish your {isTutor ? 'tutor' : 'guide'} profile when it's ready.</>
        ) : (
          <>{isTutor ? 'Teach subjects you know well and set your own rate.' : 'Show newcomers around campus, housing, paperwork and the city.'}</>
        )}
      </RoleCard>
    )
  }

  return (
    <Container className="max-w-5xl">
      <ErrorNote error={error} className="mb-4" />
      <section className="card flex flex-col gap-5 p-6 sm:flex-row sm:items-center">
        <Avatar name={user.displayName} url={user.avatarUrl} size={96} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-bold">{user.firstName} {user.lastName}</h1>
            {user.isAdmin && <Badge tone="blue"><Shield className="h-3 w-3" /> Admin</Badge>}
            {user.campusVerified && <Badge tone="green"><BadgeCheck className="h-3 w-3" /> Verified student</Badge>}
          </div>
          <p className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-ink-600">
            <span className="flex items-center gap-1"><School className="h-4 w-4" /> {user.campus?.name}</span>
            <span className="flex items-center gap-1"><Mail className="h-4 w-4" /> {user.email}</span>
            <span>Joined {formatDate(user.joinedAt)}</span>
          </p>
          {user.bio ? <p className="mt-3 text-sm text-ink-700">{user.bio}</p> : <p className="mt-3 text-sm text-ink-400">No bio yet.</p>}
        </div>
        <div className="flex shrink-0 gap-2 sm:flex-col">
          <ButtonLink to="/settings" variant="secondary" size="sm"><Pencil className="h-4 w-4" /> Edit profile</ButtonLink>
          <ButtonLink to={`/users/${user.id}`} variant="ghost" size="sm">See public view</ButtonLink>
        </div>
      </section>

      <div className="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          { icon: CalendarCheck, label: 'Upcoming sessions', value: data?.upcoming.total ?? 0, to: '/bookings' },
          { icon: BookOpen, label: 'Sessions taken', value: data?.booked.total ?? 0, to: '/bookings?as=STUDENT&status=completed' },
          { icon: GraduationCap, label: 'Sessions given', value: data?.given.total ?? 0, to: '/bookings?as=PROVIDER&status=completed' },
          { icon: Star, label: 'Your rating', value: rated.length ? `${(rated.reduce((n, p) => n + p.ratingAvg * p.ratingCount, 0) / rated.reduce((n, p) => n + p.ratingCount, 0)).toFixed(1)} ★` : '—', to: undefined },
        ].map((s) => {
          const inner = (
            <div className={cx('card p-4', s.to && 'transition hover:border-brand-300')}>
              <s.icon className="h-4 w-4 text-brand-500" />
              <p className="mt-2 text-2xl font-bold">{s.value}</p>
              <p className="text-xs text-ink-500">{s.label}</p>
            </div>
          )
          return s.to ? <Link key={s.label} to={s.to}>{inner}</Link> : <div key={s.label}>{inner}</div>
        })}
      </div>

      <h2 className="mb-3 mt-8 text-lg font-semibold">How you use Your Mate</h2>
      <p className="-mt-2 mb-4 text-sm text-ink-600">Everyone starts as a student. Tutor and guide profiles unlock once your student ID is checked.</p>
      <div className="grid gap-4 md:grid-cols-3">
        <RoleCard
          icon={<BookOpen className="h-4 w-4" />}
          title="Student"
          status={user.campusVerified ? <Badge tone="green">Active</Badge> : <Badge tone="yellow">Verify to book</Badge>}
          action={user.campusVerified ? <ButtonLink to="/tutors" variant="secondary" size="sm">Find a tutor</ButtonLink> : <ButtonLink to="/verify" size="sm">Verify now</ButtonLink>}
        >
          {user.campusVerified
            ? 'You can book tutors and guides, message them, and post on your campus feed.'
            : `Confirm you study at ${user.campus?.name ?? 'your campus'} to book sessions and post.`}
        </RoleCard>
        {providerRole('TUTOR')}
        {providerRole('GUIDE')}
      </div>

      <section className="card mt-8 p-6">
        <h2 className="font-semibold">Verification</h2>
        <ul className="mt-2 divide-y divide-ink-100">
          <Check done={!!user.emailVerified} label="Email confirmed" detail={user.email} />
          <Check
            done={user.campusVerified}
            label={`Student at ${user.campus?.name ?? 'your campus'}`}
            detail={user.campusEmailVerified ? 'Confirmed by your campus email.' : user.idVerified ? 'Confirmed by your student ID.' : 'Use your campus email, or upload your student ID.'}
            action={!user.campusVerified && <ButtonLink to="/verify" size="sm" variant="secondary">Verify</ButtonLink>}
          />
          <Check
            done={user.idVerified}
            label="Student ID checked by our team"
            detail={user.idVerified ? 'Done — you can publish tutor and guide profiles.' : idPending ? 'Waiting for review.' : data?.myVerification.status === 'REJECTED' ? `Not accepted: ${data.myVerification.reason}` : 'Needed to tutor or guide.'}
            action={!user.idVerified && !idPending && <ButtonLink to="/verify" size="sm" variant="secondary">Upload ID</ButtonLink>}
          />
        </ul>
      </section>
    </Container>
  )
}

type PublicUser = User & { providerProfiles: Provider[] }

export function UserProfile() {
  const { id = '' } = useParams()
  const { user: me } = useAuth()
  const toast = useToast()
  const navigate = useNavigate()
  const { data, loading, error } = useQuery<{ user: PublicUser | null }>(
    `query($id: ID!) { user(id: $id) { id displayName avatarUrl bio campusVerified idVerified isAdmin joinedAt
      campus { id name city country } providerProfiles { ${PROVIDER_CARD} } } }`,
    { id },
  )
  const u = data?.user
  useDocumentTitle(u?.displayName ?? 'Profile')
  const block = useAction(async () => {
    if (!window.confirm(`Block ${u?.displayName}? They won't be able to book or message you, and you won't see each other in search.`)) return
    await gql(`mutation($u: ID!) { blockUser(userId: $u) }`, { u: id })
    toast('Blocked. You can undo this in Settings.')
    navigate(-1)
  })
  if (loading) return <PageLoader />
  if (error) return <Container><ErrorNote error={error} /></Container>
  if (!u) return <Container><Empty title="This profile isn't available">The account may have been removed or suspended.</Empty></Container>
  const isMe = me?.id === u.id
  const live = u.providerProfiles.filter((p) => p.status === 'ACTIVE')

  return (
    <Container className="max-w-4xl">
      {isMe && (
        <div className="mb-4 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-sky-200 bg-sky-50 px-4 py-3 text-sm text-sky-900">
          This is how other students see you. Your email, phone and student ID are never shown here.
          <Link to="/profile" className="font-semibold underline">Back to my profile</Link>
        </div>
      )}
      <section className="card flex flex-col gap-5 p-6 sm:flex-row sm:items-center">
        <Avatar name={u.displayName} url={u.avatarUrl} size={88} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-bold">{u.displayName}</h1>
            {u.campusVerified && <Badge tone="green"><BadgeCheck className="h-3 w-3" /> Verified student</Badge>}
            {u.idVerified && <Badge tone="brand">ID checked</Badge>}
            {live.map((p) => <Badge key={p.id}>{p.kind === 'TUTOR' ? 'Tutor' : 'Guide'}</Badge>)}
          </div>
          {u.campus && (
            <p className="mt-1 flex items-center gap-1 text-sm text-ink-600">
              <MapPin className="h-4 w-4" /> {u.campus.name}{u.campus.city ? `, ${u.campus.city}` : ''} · {u.campus.country}
            </p>
          )}
          <p className="mt-1 text-xs text-ink-500">Member since {formatDate(u.joinedAt)}</p>
          {u.bio && <p className="mt-3 whitespace-pre-line text-sm text-ink-700">{u.bio}</p>}
        </div>
        {!isMe && me && (
          <div className="flex shrink-0 items-center gap-3 sm:flex-col sm:items-end">
            <MessageButton userId={u.id} />
            <ReportButton targetType="USER" targetId={u.id} label="Report" />
            <Button variant="ghost" size="sm" className="text-red-600" loading={block.pending} onClick={() => block.run()}>Block</Button>
          </div>
        )}
      </section>
      <ErrorNote error={block.error} className="mt-3" />
      {live.length > 0 && (
        <>
          <h2 className="mb-3 mt-8 text-lg font-semibold">{u.displayName.split(' ')[0]} offers</h2>
          <div className="grid gap-4 sm:grid-cols-2">{live.map((p) => <ProviderCard key={p.id} p={p} />)}</div>
        </>
      )}
    </Container>
  )
}
