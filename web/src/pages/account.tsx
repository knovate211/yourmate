import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { BadgeCheck, Camera, Clock, FileCheck2, Plus, Trash2, Upload } from 'lucide-react'
import { Container } from '../components/Layout'
import { SubjectPicker } from '../components/pickers'
import { VerificationBanner, providerPath } from '../components/domain'
import { Avatar, Badge, Button, ErrorNote, Field, Input, PageHeader, PageLoader, Select, SuccessNote, Tabs, Textarea, cx } from '../components/ui'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { gql, gqlUpload } from '../lib/graphql'
import { useAction, useDocumentTitle, useQuery } from '../lib/hooks'
import { ME, SUBJECT } from '../lib/fragments'
import { clockToMinutes, formatDate, minutesToClock, toMajor, toMinor, WEEKDAYS, zoneLabel } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import type { Availability, AvailabilityWindow, Provider, ProviderKind, SessionMode, Subject, User, Verification } from '../lib/types'

const PROFILE_FIELDS = `id kind headline bio rate { amountMinor currency } modes languages services status subjects { ${SUBJECT} } ratingAvg ratingCount sessionsCompleted
  interviewTypes companies resumeOffer { price { amountMinor currency } turnaroundDays }`

const KINDS: ProviderKind[] = ['TUTOR', 'GUIDE', 'MENTOR']
const KIND_TITLES: Record<ProviderKind, string> = { TUTOR: 'Tutor profile', GUIDE: 'Campus guide profile', MENTOR: 'Career mentor profile' }

export function ProviderEditor() {
  useDocumentTitle('My provider profile')
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const kind = KINDS.find((k) => k === params.get('kind')) ?? 'TUTOR'
  const { data, loading, refetch } = useQuery<{ myProviderProfiles: Provider[]; myAvailability: Availability }>(
    `{ myProviderProfiles { ${PROFILE_FIELDS} } myAvailability { timezone windows { weekday startMinute endMinute } } }`,
  )
  if (!user) return null
  const existing = data?.myProviderProfiles.find((p) => p.kind === kind)
  return (
    <Container className="max-w-4xl">
      <PageHeader title="Earn on Your Mate" subtitle="Set up how students find and book you." />
      <Tabs value={kind} onChange={(k) => setParams({ kind: k }, { replace: true })} tabs={KINDS.map((k) => ({ value: k, label: KIND_TITLES[k] }))} />
      <div className="mt-6 space-y-6">
        <VerificationBanner user={user} need="id" />
        {loading && !data ? (
          <PageLoader />
        ) : (
          <>
            <ProfileForm key={kind + (existing?.id ?? 'new')} kind={kind} user={user} existing={existing} onSaved={refetch} />
            <AvailabilityEditor initial={data?.myAvailability} />
          </>
        )}
      </div>
    </Container>
  )
}

function ProfileForm({ kind, user, existing, onSaved }: { kind: ProviderKind; user: User; existing?: Provider; onSaved: () => void }) {
  const { label, sessionModes, guideServices, interviewTypes } = useCatalog()
  const toast = useToast()
  const currency = existing?.rate.currency ?? user.campus?.currency ?? 'USD'
  const [headline, setHeadline] = useState(existing?.headline ?? '')
  const [bio, setBio] = useState(existing?.bio ?? '')
  const [rate, setRate] = useState(existing ? String(toMajor(existing.rate.amountMinor, currency)) : '')
  const [modes, setModes] = useState<SessionMode[]>(existing?.modes ?? ['ONLINE', 'IN_PERSON'])
  const [languages, setLanguages] = useState((existing?.languages ?? ['English']).join(', '))
  const [subjects, setSubjects] = useState<string[]>(existing?.subjects.map((s) => s.id) ?? [])
  const [services, setServices] = useState<string[]>(existing?.services ?? [])
  const [types, setTypes] = useState<string[]>(existing?.interviewTypes ?? [])
  const [companies, setCompanies] = useState((existing?.companies ?? []).join(', '))
  const [resumeOn, setResumeOn] = useState(!!existing?.resumeOffer)
  const [resumePrice, setResumePrice] = useState(existing?.resumeOffer ? String(toMajor(existing.resumeOffer.price.amountMinor, currency)) : '')
  const [turnaround, setTurnaround] = useState(existing?.resumeOffer?.turnaroundDays ?? 3)
  const [newSubject, setNewSubject] = useState({ name: '', code: '' })

  const save = useAction(async (publish: boolean) => {
    await gql(`mutation($i: ProviderProfileInput!) { saveProviderProfile(input: $i) { id status } }`, {
      i: {
        kind,
        headline,
        bio,
        rateMinor: toMinor(Number(rate || 0), currency),
        modes,
        languages: languages.split(',').map((l) => l.trim()).filter(Boolean),
        subjectIds: kind === 'TUTOR' ? subjects : [],
        services: kind === 'GUIDE' ? services : [],
        ...(kind === 'MENTOR' && {
          interviewTypes: types,
          companies: companies.split(',').map((c) => c.trim()).filter(Boolean),
          resumeOffer: resumeOn ? { priceMinor: toMinor(Number(resumePrice || 0), currency), turnaroundDays: turnaround } : null,
        }),
        publish: publish || existing?.status === 'ACTIVE',
      },
    })
    toast(publish ? 'Your profile is live!' : 'Saved.')
    onSaved()
  })
  const status = useAction(async (s: string) => {
    await gql(`mutation($id: ID!, $s: ProviderStatus!) { setProviderStatus(id: $id, status: $s) { status } }`, { id: existing!.id, s })
    toast(s === 'PAUSED' ? 'Profile paused; you won’t get new requests.' : 'Profile is live again.')
    onSaved()
  })
  const addSubject = useAction(async () => {
    const d = await gql<{ createSubject: Subject }>(`mutation($i: SubjectInput!) { createSubject(input: $i) { ${SUBJECT} } }`, { i: newSubject })
    setSubjects((s) => [...s, d.createSubject.id])
    setNewSubject({ name: '', code: '' })
    toast(`Added ${d.createSubject.name} for your campus.`)
  })
  const toggle = <T,>(list: T[], v: T) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v])

  return (
    <form className="card space-y-5 p-6" onSubmit={(e) => { e.preventDefault(); save.run(false) }}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-lg font-semibold">{KIND_TITLES[kind]}</h2>
        {existing && (
          <div className="flex items-center gap-2">
            <Badge tone={existing.status === 'ACTIVE' ? 'green' : existing.status === 'SUSPENDED' ? 'red' : 'yellow'}>{existing.status.toLowerCase()}</Badge>
            {existing.status === 'ACTIVE' && <Link to={providerPath(existing)} className="text-sm link">View public page</Link>}
          </div>
        )}
      </div>
      {existing?.status === 'SUSPENDED' && <ErrorNote error={new Error('An admin suspended this profile after a review. Contact us to appeal.')} />}
      <Field label="Headline" hint={`${headline.length}/120 · One line students see in search, e.g. ${kind === 'MENTOR' ? '“SDE intern at Amazon · coding and HR mock interviews”' : '“Final-year CSE · DSA, Java and placement prep”'}.`}>
        <Input required minLength={10} maxLength={120} value={headline} onChange={(e) => setHeadline(e.target.value)} />
      </Field>
      <Field label="About you" hint={`${bio.length}/2000 · Your experience, grades, how you teach or guide, and what students can expect.`}>
        <Textarea required minLength={30} maxLength={2000} rows={6} value={bio} onChange={(e) => setBio(e.target.value)} />
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={kind === 'MENTOR' ? `Mock interview rate per hour (${currency})` : `Hourly rate (${currency})`} hint="Enter 0 to offer sessions for free. Students pay you directly.">
          <Input type="number" min={0} step="any" required value={rate} onChange={(e) => setRate(e.target.value)} />
        </Field>
        <Field label="Languages" hint="Comma-separated.">
          <Input value={languages} onChange={(e) => setLanguages(e.target.value)} />
        </Field>
      </div>
      <div>
        <p className="label">Session formats</p>
        <div className="flex gap-2">
          {sessionModes.map(({ value }) => value as SessionMode).map((m) => (
            <button type="button" key={m} onClick={() => setModes(toggle(modes, m))} className={cx('rounded-lg border px-3 py-1.5 text-sm font-medium', modes.includes(m) ? 'border-brand-500 bg-brand-50 text-brand-800' : 'border-ink-200 text-ink-700')}>
              {label('sessionModes', m)}
            </button>
          ))}
        </div>
      </div>
      {kind === 'MENTOR' ? (
        <div className="space-y-5">
          <div>
            <p className="label">Mock interviews you run</p>
            <div className="flex flex-wrap gap-2">
              {interviewTypes.map(({ value: v, label: l }) => (
                <button type="button" key={v} onClick={() => setTypes(toggle(types, v))} className={cx('rounded-full border px-3 py-1 text-sm font-medium', types.includes(v) ? 'border-brand-500 bg-brand-500 text-white' : 'border-ink-200 text-ink-700')}>
                  {l}
                </button>
              ))}
            </div>
            <p className="mt-1 text-xs text-ink-500">Leave empty if you only review resumes. Students book interviews in your weekly hours below.</p>
          </div>
          <Field label="Where you've worked, interned or been placed" hint="Comma-separated, up to 6. Students search by company.">
            <Input value={companies} onChange={(e) => setCompanies(e.target.value)} placeholder="e.g. Amazon, Deloitte, Zomato" />
          </Field>
          <div className="rounded-xl border border-ink-200 p-4">
            <label className="flex items-center gap-2 text-sm font-medium">
              <input type="checkbox" checked={resumeOn} onChange={(e) => setResumeOn(e.target.checked)} className="h-4 w-4 accent-brand-500" />
              Review resumes
            </label>
            {resumeOn && (
              <div className="mt-3 grid gap-4 sm:grid-cols-2">
                <Field label={`Price per review (${currency})`} hint="Enter 0 to review for free.">
                  <Input type="number" min={0} step="any" required value={resumePrice} onChange={(e) => setResumePrice(e.target.value)} />
                </Field>
                <Field label="Turnaround" hint="Days you take to send feedback after accepting.">
                  <Select value={turnaround} onChange={(e) => setTurnaround(Number(e.target.value))}>
                    {[1, 2, 3, 5, 7, 10, 14].map((d) => <option key={d} value={d}>{d} day{d > 1 ? 's' : ''}</option>)}
                  </Select>
                </Field>
              </div>
            )}
          </div>
        </div>
      ) : kind === 'TUTOR' ? (
        <div>
          <p className="label">Subjects you teach</p>
          <SubjectPicker campusId={user.campus?.id} value={subjects} onChange={setSubjects} />
          {user.idVerified && (
            <div className="mt-2 flex flex-col gap-2 sm:flex-row">
              <Input placeholder="Missing a course? Name" value={newSubject.name} onChange={(e) => setNewSubject({ ...newSubject, name: e.target.value })} />
              <Input placeholder="Code (optional)" className="sm:w-40" value={newSubject.code} onChange={(e) => setNewSubject({ ...newSubject, code: e.target.value })} />
              <Button type="button" variant="secondary" disabled={newSubject.name.length < 2} loading={addSubject.pending} onClick={() => addSubject.run()}><Plus className="h-4 w-4" /> Add</Button>
            </div>
          )}
          <ErrorNote error={addSubject.error} className="mt-2" />
        </div>
      ) : (
        <div>
          <p className="label">What you can help with</p>
          <div className="flex flex-wrap gap-2">
            {guideServices.map(({ value: v, label: l }) => (
              <button type="button" key={v} onClick={() => setServices(toggle(services, v))} className={cx('rounded-full border px-3 py-1 text-sm font-medium', services.includes(v) ? 'border-brand-500 bg-brand-500 text-white' : 'border-ink-200 text-ink-700')}>
                {l}
              </button>
            ))}
          </div>
        </div>
      )}
      <ErrorNote error={save.error || status.error} />
      <div className="flex flex-wrap gap-2 border-t border-ink-100 pt-4">
        {existing?.status !== 'ACTIVE' && existing?.status !== 'SUSPENDED' && (
          <Button type="button" disabled={!user.idVerified} loading={save.pending} onClick={() => save.run(true)}>
            <BadgeCheck className="h-4 w-4" /> Save & publish
          </Button>
        )}
        <Button type="submit" variant={existing?.status === 'ACTIVE' ? 'primary' : 'secondary'} loading={save.pending}>{existing?.status === 'ACTIVE' ? 'Save changes' : 'Save draft'}</Button>
        {existing?.status === 'ACTIVE' && <Button type="button" variant="ghost" loading={status.pending} onClick={() => status.run('PAUSED')}>Pause profile</Button>}
        {existing?.status === 'PAUSED' && <Button type="button" variant="secondary" disabled={!user.idVerified} loading={status.pending} onClick={() => status.run('ACTIVE')}>Resume</Button>}
      </div>
      {!user.idVerified && <p className="text-xs text-ink-500">You can save a draft now and publish once your student ID is verified.</p>}
    </form>
  )
}

function AvailabilityEditor({ initial }: { initial?: Availability }) {
  const toast = useToast()
  const [windows, setWindows] = useState<AvailabilityWindow[]>(initial?.windows ?? [])
  const tz = initial?.timezone ?? 'UTC'
  useEffect(() => {
    setWindows(initial?.windows ?? [])
  }, [initial])
  const save = useAction(async () => {
    await gql(`mutation($w: [AvailabilityInput!]!) { setAvailability(windows: $w) { timezone } }`, { w: windows.map(({ weekday, startMinute, endMinute }) => ({ weekday, startMinute, endMinute })) })
    toast('Availability saved.')
  })
  const update = (i: number, patch: Partial<AvailabilityWindow>) => setWindows((ws) => ws.map((w, j) => (j === i ? { ...w, ...patch } : w)))
  const times = Array.from({ length: 49 }, (_, i) => i * 30)
  const order = [1, 2, 3, 4, 5, 6, 0]
  return (
    <section className="card p-6">
      <h2 className="flex items-center gap-2 text-lg font-semibold"><Clock className="h-5 w-5 text-brand-500" /> Weekly availability</h2>
      <p className="mt-1 text-sm text-ink-600">Students can request sessions inside these hours, at least 2 hours ahead. Times are in {tz} ({zoneLabel(tz)}), your campus timezone.</p>
      <div className="mt-4 divide-y divide-ink-100">
        {order.map((day) => {
          const list = windows.map((w, i) => ({ w, i })).filter(({ w }) => w.weekday === day)
          return (
            <div key={day} className="flex flex-col gap-2 py-3 sm:flex-row sm:items-start">
              <p className="w-28 pt-2 text-sm font-medium">{WEEKDAYS[day]}</p>
              <div className="flex-1 space-y-2">
                {list.length === 0 && <p className="pt-2 text-sm text-ink-400">Unavailable</p>}
                {list.map(({ w, i }) => (
                  <div key={i} className="flex items-center gap-2">
                    <Select className="w-28" value={minutesToClock(w.startMinute)} onChange={(e) => update(i, { startMinute: clockToMinutes(e.target.value) })} aria-label="From">
                      {times.slice(0, -1).map((t) => <option key={t}>{minutesToClock(t)}</option>)}
                    </Select>
                    <span className="text-ink-400">–</span>
                    <Select className="w-28" value={w.endMinute === 1440 ? '24:00' : minutesToClock(w.endMinute)} onChange={(e) => update(i, { endMinute: e.target.value === '24:00' ? 1440 : clockToMinutes(e.target.value) })} aria-label="To">
                      {times.slice(1).map((t) => <option key={t}>{t === 1440 ? '24:00' : minutesToClock(t)}</option>)}
                    </Select>
                    <button type="button" onClick={() => setWindows((ws) => ws.filter((_, j) => j !== i))} className="rounded-lg p-2 text-ink-400 hover:bg-ink-100 hover:text-red-600" aria-label="Remove">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                ))}
              </div>
              <button type="button" onClick={() => setWindows((ws) => [...ws, { weekday: day, startMinute: 17 * 60, endMinute: 20 * 60 }])} className="self-start pt-2 text-sm link">
                + Add hours
              </button>
            </div>
          )
        })}
      </div>
      <ErrorNote error={save.error} className="mt-3" />
      <Button className="mt-4" loading={save.pending} onClick={() => save.run()}>Save availability</Button>
    </section>
  )
}

export function Verify() {
  useDocumentTitle('Verification')
  const { user, reload } = useAuth()
  const toast = useToast()
  const [file, setFile] = useState<File | null>(null)
  const { data, loading, refetch } = useQuery<{ myVerification: Verification }>(`{ myVerification { id status reason createdAt reviewedAt } }`)
  const upload = useAction(async () => {
    await gqlUpload(`mutation($d: Upload!) { submitVerification(document: $d) { status } }`, {}, { d: file! })
    setFile(null)
    toast('Uploaded. We’ll review it soon.')
    refetch()
  })
  useEffect(() => {
    if (data?.myVerification.status === 'APPROVED' && !user?.idVerified) reload()
  }, [data, user, reload])
  if (!user) return null
  const v = data?.myVerification
  return (
    <Container className="max-w-3xl">
      <PageHeader title="Verification" subtitle="Your Mate is for real students only. Here's where you stand." />
      <div className="space-y-4">
        <div className="card p-6">
          <ul className="space-y-4">
            <li className="flex items-start gap-3">
              <BadgeCheck className={cx('mt-0.5 h-5 w-5', user.emailVerified ? 'text-emerald-500' : 'text-ink-300')} />
              <div><p className="font-medium">Email confirmed</p><p className="text-sm text-ink-600">{user.email}</p></div>
            </li>
            <li className="flex items-start gap-3">
              <BadgeCheck className={cx('mt-0.5 h-5 w-5', user.campusVerified ? 'text-emerald-500' : 'text-ink-300')} />
              <div>
                <p className="font-medium">Campus member{user.campus ? ` · ${user.campus.name}` : ''}</p>
                <p className="text-sm text-ink-600">
                  {user.campusEmailVerified ? 'Verified with your campus email.' : user.idVerified ? 'Verified with your student ID.' : user.campus?.status === 'PENDING' ? 'Your campus is still being reviewed.' : 'Verify with your student ID below to book sessions and post.'}
                </p>
              </div>
            </li>
            <li className="flex items-start gap-3">
              <BadgeCheck className={cx('mt-0.5 h-5 w-5', user.idVerified ? 'text-emerald-500' : 'text-ink-300')} />
              <div><p className="font-medium">Student ID checked</p><p className="text-sm text-ink-600">Needed to publish a tutor or guide profile.</p></div>
            </li>
          </ul>
        </div>
        {!user.idVerified && (
          <div className="card p-6">
            <h2 className="flex items-center gap-2 font-semibold"><FileCheck2 className="h-5 w-5 text-brand-500" /> Upload your student ID</h2>
            {loading ? (
              <PageLoader />
            ) : v?.status === 'PENDING' ? (
              <SuccessNote className="mt-3">We received your ID on {formatDate(v.createdAt!)} and will review it soon. We'll email you the result.</SuccessNote>
            ) : (
              <>
                {v?.status === 'REJECTED' && <ErrorNote className="mt-3" error={new Error(`Your last upload wasn't accepted: ${v.reason}`)} />}
                <p className="mt-2 text-sm text-ink-600">
                  A clear photo or PDF of your current student ID card showing your name, campus and validity. Only our verification team sees it, through a link that expires after 15 minutes, and the file is deleted as soon as it's reviewed.
                </p>
                <label className="mt-4 flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed border-ink-300 px-4 py-8 text-center hover:border-brand-400">
                  <Upload className="h-6 w-6 text-ink-400" />
                  <span className="mt-2 text-sm font-medium">{file ? file.name : 'Choose a JPEG, PNG, WebP or PDF (max 5 MB)'}</span>
                  <input type="file" accept="image/jpeg,image/png,image/webp,application/pdf" className="sr-only" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
                </label>
                <ErrorNote error={upload.error} className="mt-3" />
                <Button className="mt-4" disabled={!file} loading={upload.pending} onClick={() => upload.run()}>Submit for review</Button>
              </>
            )}
          </div>
        )}
      </div>
    </Container>
  )
}

export function Settings() {
  useDocumentTitle('Settings')
  const { user, setUser } = useAuth()
  const toast = useToast()
  const fileRef = useRef<HTMLInputElement>(null)
  const [form, setForm] = useState({ firstName: user?.firstName ?? '', lastName: user?.lastName ?? '', phone: user?.phone ?? '', gender: user?.gender ?? '', bio: user?.bio ?? '', studentIdNo: user?.studentIdNo ?? '' })
  const [pw, setPw] = useState({ current: '', next: '' })
  const { data: blocked, refetch: refetchBlocked } = useQuery<{ blockedUsers: User[] }>(`{ blockedUsers { id displayName avatarUrl } }`)
  const save = useAction(async () => {
    const d = await gql<{ updateProfile: User }>(`mutation($i: UpdateProfileInput!) { updateProfile(input: $i) { ${ME} } }`, { i: form })
    setUser(d.updateProfile)
    toast('Profile updated.')
  })
  const avatar = useAction(async (f: File) => {
    const d = await gqlUpload<{ uploadAvatar: User }>(`mutation($f: Upload!) { uploadAvatar(file: $f) { ${ME} } }`, {}, { f })
    setUser(d.uploadAvatar)
    toast('Photo updated.')
  })
  const changePw = useAction(async () => {
    await gql(`mutation($c: String!, $n: String!) { changePassword(currentPassword: $c, newPassword: $n) }`, { c: pw.current, n: pw.next })
    setPw({ current: '', next: '' })
    toast('Password changed.')
  })
  const unblock = useAction(async (id: string) => {
    await gql(`mutation($u: ID!) { unblockUser(userId: $u) }`, { u: id })
    refetchBlocked()
  })
  if (!user) return null
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value })
  return (
    <Container className="max-w-3xl">
      <PageHeader title="Settings" />
      <div className="space-y-6">
        <form className="card space-y-4 p-6" onSubmit={(e) => { e.preventDefault(); save.run() }}>
          <h2 className="font-semibold">Profile</h2>
          <div className="flex items-center gap-4">
            <Avatar name={user.displayName} url={user.avatarUrl} size={72} />
            <div>
              <Button type="button" variant="secondary" size="sm" loading={avatar.pending} onClick={() => fileRef.current?.click()}><Camera className="h-4 w-4" /> Change photo</Button>
              <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" className="sr-only" onChange={(e) => e.target.files?.[0] && avatar.run(e.target.files[0])} />
              <p className="mt-1 text-xs text-ink-500">Square images look best. Max 5 MB.</p>
            </div>
          </div>
          <ErrorNote error={avatar.error} />
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="First name"><Input required value={form.firstName} onChange={set('firstName')} /></Field>
            <Field label="Last name" hint="Others see only your initial."><Input value={form.lastName} onChange={set('lastName')} /></Field>
            <Field label="Phone" hint="Shared only with people whose session you've accepted, or who accepted yours."><Input type="tel" value={form.phone} onChange={set('phone')} /></Field>
            <Field label="Gender">
              <Select value={form.gender} onChange={set('gender')}>
                <option value="">Prefer not to say</option><option>Female</option><option>Male</option><option>Non-binary</option>
              </Select>
            </Field>
            <Field label="Student ID / registration number" hint="Private."><Input value={form.studentIdNo} onChange={set('studentIdNo')} /></Field>
            <Field label="Campus"><Input disabled value={user.campus?.name ?? ''} /></Field>
          </div>
          <Field label="Short bio"><Textarea value={form.bio} onChange={set('bio')} maxLength={1000} rows={3} /></Field>
          <ErrorNote error={save.error} />
          <Button type="submit" loading={save.pending}>Save profile</Button>
        </form>

        <form className="card space-y-4 p-6" onSubmit={(e) => { e.preventDefault(); changePw.run() }}>
          <h2 className="font-semibold">Password</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Current password"><Input type="password" autoComplete="current-password" required value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} /></Field>
            <Field label="New password" hint="At least 8 characters."><Input type="password" autoComplete="new-password" minLength={8} required value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} /></Field>
          </div>
          <ErrorNote error={changePw.error} />
          <Button type="submit" variant="secondary" loading={changePw.pending}>Change password</Button>
        </form>

        <section className="card p-6">
          <h2 className="font-semibold">Blocked people</h2>
          <p className="mt-1 text-sm text-ink-600">Blocked people can't book you, message you, or appear in your searches.</p>
          {blocked?.blockedUsers.length ? (
            <ul className="mt-3 divide-y divide-ink-100">
              {blocked.blockedUsers.map((b) => (
                <li key={b.id} className="flex items-center justify-between py-2">
                  <span className="flex items-center gap-2 text-sm"><Avatar name={b.displayName} url={b.avatarUrl} size={28} /> {b.displayName}</span>
                  <Button size="sm" variant="ghost" onClick={() => unblock.run(b.id)}>Unblock</Button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-3 text-sm text-ink-400">Nobody.</p>
          )}
        </section>
      </div>
    </Container>
  )
}
