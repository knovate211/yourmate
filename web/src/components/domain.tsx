import { useState } from 'react'
import { Link } from 'react-router-dom'
import { BadgeCheck, Briefcase, Flag, Globe2, MapPin, ShieldAlert } from 'lucide-react'
import { gql } from '../lib/graphql'
import { useAction } from '../lib/hooks'
import { formatMoney, plural, replyLabel, slotLabel } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import type { BookingStatus, Provider, ProviderKind, User } from '../lib/types'
import { useAuth } from '../lib/auth'
import { Avatar, Badge, Button, ErrorNote, Field, Modal, Select, Stars, Textarea, type Tone } from './ui'
import { useToast } from './toast'

const KIND_PATHS: Record<ProviderKind, string> = { TUTOR: 'tutors', GUIDE: 'guides', MENTOR: 'mentors' }

export function providerPath(p: Pick<Provider, 'id' | 'kind'>) {
  return `/${KIND_PATHS[p.kind]}/${p.id}`
}

/** What a provider offers, as short tags for cards and profiles. */
export function useProviderTags() {
  const { label } = useCatalog()
  return (p: Provider) => {
    if (p.kind === 'TUTOR') return p.subjects.map((s) => s.name)
    if (p.kind === 'GUIDE') return p.services.map((s) => label('guideServices', s))
    return [...p.interviewTypes.map((t) => label('interviewTypes', t)), ...(p.resumeOffer ? ['Resume review'] : [])]
  }
}

export function ProviderCard({ p }: { p: Provider }) {
  const { label } = useCatalog()
  const tags = useProviderTags()(p)
  return (
    <Link to={providerPath(p)} className="card group flex flex-col p-5 transition hover:-translate-y-0.5 hover:border-brand-300 hover:shadow-md">
      <div className="flex items-start gap-3">
        <Avatar name={p.displayName} url={p.avatarUrl} size={52} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <h3 className="truncate font-semibold text-ink-900 group-hover:text-brand-700">{p.displayName}</h3>
            <BadgeCheck className="h-4 w-4 shrink-0 text-brand-500" aria-label="Verified student" />
          </div>
          {p.campus && (
            <p className="flex items-center gap-1 truncate text-xs text-ink-500">
              <MapPin className="h-3 w-3" /> {p.campus.name}
            </p>
          )}
          <div className="mt-1 flex items-center gap-1.5 text-xs text-ink-600">
            {p.ratingCount > 0 ? (
              <>
                <Stars value={p.ratingAvg} size={12} />
                <span className="font-medium">{p.ratingAvg.toFixed(1)}</span>
                <span className="text-ink-400">({p.ratingCount})</span>
              </>
            ) : (
              <span className="text-ink-400">New on Your Mate</span>
            )}
          </div>
        </div>
        <div className="text-right">
          <p className="font-bold text-ink-900">{formatMoney(p.rate)}</p>
          {p.rate.amountMinor > 0 && <p className="text-[11px] text-ink-500">per hour</p>}
        </div>
      </div>
      <p className="mt-3 line-clamp-2 text-sm text-ink-700">{p.headline}</p>
      {(p.nextSlotAt || p.replyMinutes) && (
        <p className="mt-2 flex flex-wrap justify-between gap-x-3 text-xs">
          {p.replyMinutes ? <span className="text-ink-500">Replies in {replyLabel(p.replyMinutes)}</span> : <span />}
          {p.nextSlotAt && <span className="font-semibold text-brand-600">Next: {slotLabel(p.nextSlotAt)}</span>}
        </p>
      )}
      {p.companies.length > 0 && (
        <p className="mt-2 flex items-center gap-1 truncate text-xs text-ink-600">
          <Briefcase className="h-3.5 w-3.5 shrink-0 text-ink-400" /> {p.companies.join(' · ')}
        </p>
      )}
      <div className="mt-3 flex flex-wrap gap-1.5">
        {tags.slice(0, 4).map((t) => (
          <span key={t} className="chip">{t}</span>
        ))}
        {tags.length > 4 && <span className="chip">+{tags.length - 4}</span>}
      </div>
      <div className="mt-auto flex items-center gap-3 pt-4 text-xs text-ink-500">
        {p.modes.map((m) => (
          <span key={m} className="flex items-center gap-1">
            {m === 'ONLINE' ? <Globe2 className="h-3.5 w-3.5" /> : <MapPin className="h-3.5 w-3.5" />} {label('sessionModes', m)}
          </span>
        ))}
        {p.sessionsCompleted > 0 && <span className="ml-auto">{plural(p.sessionsCompleted, p.kind === 'MENTOR' ? 'interview' : 'session')}</span>}
      </div>
    </Link>
  )
}

const bookingTones: Record<BookingStatus, Tone> = {
  REQUESTED: 'yellow',
  ACCEPTED: 'green',
  DECLINED: 'red',
  CANCELLED: 'gray',
  COMPLETED: 'blue',
  EXPIRED: 'gray',
}

export function BookingStatusBadge({ status }: { status: BookingStatus }) {
  const label = status === 'REQUESTED' ? 'Awaiting reply' : status.charAt(0) + status.slice(1).toLowerCase()
  return <Badge tone={bookingTones[status]}>{label}</Badge>
}

export function ReportButton({ targetType, targetId, label = 'Report' }: { targetType: 'USER' | 'PROVIDER' | 'REVIEW' | 'POST' | 'BOOKING' | 'COMMUNITY' | 'COMMUNITY_POST'; targetId: string; label?: string }) {
  const { user } = useAuth()
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const { reportReasons } = useCatalog()
  const [reason, setReason] = useState<string>('')
  const [details, setDetails] = useState('')
  const send = useAction(async () => {
    await gql(`mutation($i: ReportInput!) { report(input: $i) }`, { i: { targetType, targetId, reason: reason || reportReasons[0]?.value, details } })
    setOpen(false)
    setDetails('')
    toast('Thanks — our team will review this.')
  })
  if (!user) return null
  return (
    <>
      <button onClick={() => setOpen(true)} className="inline-flex items-center gap-1 text-xs font-medium text-ink-500 hover:text-red-600">
        <Flag className="h-3.5 w-3.5" /> {label}
      </button>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Report a problem"
        footer={
          <>
            <Button variant="secondary" onClick={() => setOpen(false)}>Cancel</Button>
            <Button variant="danger" loading={send.pending} onClick={() => send.run()}>Send report</Button>
          </>
        }
      >
        <div className="space-y-4">
          <p className="text-sm text-ink-600">Reports are private. The person you report is not told who sent it.</p>
          <Field label="What's wrong?">
            <Select value={reason || reportReasons[0]?.value || ''} onChange={(e) => setReason(e.target.value)}>
              {reportReasons.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
            </Select>
          </Field>
          <Field label="Details" hint={reason === 'OTHER' ? 'Required for “Something else”.' : 'Optional, but helps us act faster.'}>
            <Textarea value={details} onChange={(e) => setDetails(e.target.value)} maxLength={2000} />
          </Field>
          <ErrorNote error={send.error} />
        </div>
      </Modal>
    </>
  )
}

/** Explains what the viewer still needs to verify, if anything. */
export function VerificationBanner({ user, need }: { user: User; need: 'campus' | 'id' }) {
  if (need === 'campus' && user.campusVerified) return null
  if (need === 'id' && user.idVerified) return null
  const campusPending = user.campus?.status === 'PENDING'
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex items-start gap-3">
        <ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-amber-600" />
        <div className="text-sm text-amber-900">
          {campusPending ? (
            <p><strong>{user.campus?.name}</strong> is still being reviewed. We'll email you when it's live; you can verify your student ID meanwhile.</p>
          ) : need === 'campus' ? (
            <p>
              <strong>Verify you study at {user.campus?.name}</strong> to book sessions and post. Sign up with your campus email
              {user.campus?.emailDomains.length ? ` (@${user.campus.emailDomains[0]})` : ''}, or upload your student ID.
            </p>
          ) : (
            <p><strong>Verify your student ID</strong> to publish a tutor or guide profile. An admin reviews it, usually within a day.</p>
          )}
        </div>
      </div>
      <Link to="/verify" className="shrink-0 rounded-lg bg-amber-600 px-3 py-1.5 text-center text-sm font-semibold text-white hover:bg-amber-700">
        Verify now
      </Link>
    </div>
  )
}
