import { useEffect, useState, type ReactNode } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { BookOpen, Briefcase, Compass, GraduationCap, MailCheck } from 'lucide-react'
import { Logo } from '../components/Layout'
import { CampusPicker } from '../components/pickers'
import { Button, ErrorNote, Field, Input, Select, SuccessNote, cx } from '../components/ui'
import { useAuth } from '../lib/auth'
import { gql, GqlError } from '../lib/graphql'
import { useAction, useDocumentTitle, useQuery } from '../lib/hooks'
import { CAMPUS } from '../lib/fragments'
import type { Campus } from '../lib/types'

function AuthShell({ title, subtitle, children, footer }: { title: string; subtitle?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  return (
    <div className="flex min-h-[calc(100vh-4rem)] items-start justify-center bg-gradient-to-b from-brand-50 to-ink-50 px-4 py-10 sm:items-center">
      <div className="w-full max-w-md">
        <div className="card p-6 shadow-sm sm:p-8">
          <Logo className="mb-6" />
          <h1 className="text-2xl font-bold">{title}</h1>
          {subtitle && <p className="mt-1 text-sm text-ink-600">{subtitle}</p>}
          <div className="mt-6">{children}</div>
        </div>
        {footer && <p className="mt-4 text-center text-sm text-ink-600">{footer}</p>}
      </div>
    </div>
  )
}

// Running locally, no real email is sent: Mailpit catches every message.
const LOCAL_INBOX = import.meta.env.VITE_MAIL_INBOX_URL ?? 'http://localhost:8027'
const isLocal = import.meta.env.DEV || ['localhost', '127.0.0.1'].includes(window.location.hostname)

function LocalInboxHint() {
  if (!isLocal) return null
  return (
    <div className="mb-4 rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-sm text-sky-900">
      <strong>Local development:</strong> emails aren't really sent. Find your code in the local inbox at{' '}
      <a href={LOCAL_INBOX} target="_blank" rel="noopener noreferrer" className="font-semibold underline">
        {LOCAL_INBOX.replace(/^https?:\/\//, '')}
      </a>
      .
    </div>
  )
}

type Intent = 'learn' | 'teach' | 'guide' | 'mentor'

// Signup intent only decides where you land after verifying your email.
// It grants nothing: booking needs campus verification and teaching or
// guiding needs an admin-approved student ID, both enforced by the backend.
const INTENTS: { value: Intent; label: string; icon: typeof BookOpen; next: string }[] = [
  { value: 'learn', label: 'Learn', icon: BookOpen, next: '/dashboard' },
  { value: 'teach', label: 'Teach', icon: GraduationCap, next: '/dashboard/profile' },
  { value: 'guide', label: 'Guide', icon: Compass, next: '/dashboard/profile?kind=GUIDE' },
  { value: 'mentor', label: 'Mentor', icon: Briefcase, next: '/dashboard/profile?kind=MENTOR' },
]

function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') ? next : '/dashboard'
}

export function Login() {
  useDocumentTitle('Log in')
  const { user, login } = useAuth()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const next = safeNext(params.get('next'))
  const act = useAction(async () => {
    try {
      await login(email, password)
      navigate(next, { replace: true })
    } catch (e) {
      if (e instanceof GqlError && e.reason === 'EMAIL_NOT_VERIFIED') {
        await gql(`mutation($e: String!) { resendVerification(email: $e) }`, { e: email }).catch(() => {})
        navigate(`/verify-email?email=${encodeURIComponent(email)}&next=${encodeURIComponent(next)}`)
        return
      }
      throw e
    }
  })
  if (user) return <Navigate to={next} replace />
  return (
    <AuthShell title="Welcome back" subtitle="Log in to book sessions and manage your profile." footer={<>New to Your Mate? <Link to="/signup" className="link">Create an account</Link></>}>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); act.run() }}>
        <Field label="Email" htmlFor="l-email">
          <Input id="l-email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label="Password" htmlFor="l-pass">
          <Input id="l-pass" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <div className="flex justify-end">
          <Link to={`/forgot-password${email ? `?email=${encodeURIComponent(email)}` : ''}`} className="text-sm link">Forgot password?</Link>
        </div>
        <ErrorNote error={act.error} />
        <Button type="submit" className="w-full" size="lg" loading={act.pending}>Log in</Button>
      </form>
    </AuthShell>
  )
}

export function Signup() {
  useDocumentTitle('Create account')
  const { user } = useAuth()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [campus, setCampus] = useState<Campus | null>(null)
  const [intent, setIntent] = useState<Intent>(() => {
    const i = params.get('intent')
    return INTENTS.find((x) => x.value === i)?.value ?? 'learn'
  })
  const [form, setForm] = useState({ firstName: '', lastName: '', email: '', password: '', studentIdNo: '', gender: '', phone: '' })
  const preset = params.get('campus')
  const { data: presetCampus } = useQuery<{ campus: Campus | null }>(`query($id: ID) { campus(id: $id) { ${CAMPUS} } }`, { id: preset }, { skip: !preset })
  useEffect(() => {
    if (presetCampus?.campus) setCampus(presetCampus.campus)
  }, [presetCampus])

  const domain = form.email.split('@')[1]?.toLowerCase() ?? ''
  const domainMatch = !!campus && campus.emailDomains.some((d) => domain === d || domain.endsWith('.' + d))
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value })

  const act = useAction(async () => {
    if (!campus) throw new Error('Choose your campus.')
    const d = await gql<{ register: { email: string } }>(`mutation($i: RegisterInput!) { register(input: $i) { email } }`, { i: { ...form, campusId: campus.id } })
    const next = INTENTS.find((x) => x.value === intent)!.next
    navigate(`/verify-email?email=${encodeURIComponent(d.register.email)}&next=${encodeURIComponent(next)}`)
  })
  if (user) return <Navigate to="/dashboard" replace />

  return (
    <AuthShell title="Join Your Mate" subtitle="Free for students. Takes a minute." footer={<>Already have an account? <Link to="/login" className="link">Log in</Link></>}>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); act.run() }}>
        <fieldset>
          <legend className="label">What brings you here?</legend>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4" role="radiogroup">
            {INTENTS.map((i) => (
              <button
                type="button"
                key={i.value}
                role="radio"
                aria-checked={intent === i.value}
                onClick={() => setIntent(i.value)}
                className={cx(
                  'flex flex-col items-center gap-1 rounded-xl border px-2 py-3 text-center text-sm font-medium transition',
                  intent === i.value ? 'border-brand-500 bg-brand-50 text-brand-800' : 'border-ink-200 text-ink-700 hover:border-ink-300',
                )}
              >
                <i.icon className="h-5 w-5" />
                {i.label}
              </button>
            ))}
          </div>
          <p className="mt-1.5 text-xs text-ink-500">
            {intent === 'learn'
              ? 'Book tutors and campus guides. You can start teaching any time later.'
              : `Every account starts as a student. To go live as a ${{ teach: 'tutor', guide: 'guide', mentor: 'career mentor' }[intent]}, we'll check your student ID after signup.`}
          </p>
        </fieldset>
        <Field label="Your campus" htmlFor="s-campus">
          <CampusPicker id="s-campus" value={campus} onChange={setCampus} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="First name" htmlFor="s-first">
            <Input id="s-first" required autoComplete="given-name" value={form.firstName} onChange={set('firstName')} />
          </Field>
          <Field label="Last name" htmlFor="s-last">
            <Input id="s-last" autoComplete="family-name" value={form.lastName} onChange={set('lastName')} />
          </Field>
        </div>
        <Field
          label="Email"
          htmlFor="s-email"
          hint={
            campus && campus.emailDomains.length > 0 ? (
              domainMatch ? (
                <span className="text-emerald-700">Campus email: you'll be verified as a {campus.name} student automatically.</span>
              ) : (
                <>Use your @{campus.emailDomains[0]} address to be verified instantly. Personal emails work too, but you'll need to upload a student ID.</>
              )
            ) : campus ? (
              <>{campus.name} has no campus email on file; you'll verify with your student ID.</>
            ) : undefined
          }
        >
          <Input id="s-email" type="email" required autoComplete="email" value={form.email} onChange={set('email')} />
        </Field>
        <Field label="Password" htmlFor="s-pass" hint="At least 8 characters.">
          <Input id="s-pass" type="password" required minLength={8} autoComplete="new-password" value={form.password} onChange={set('password')} />
        </Field>
        <details className="rounded-lg border border-ink-200 px-3 py-2 text-sm">
          <summary className="cursor-pointer font-medium text-ink-700">Optional details</summary>
          <div className="mt-3 space-y-3">
            <Field label="Student ID / registration number" htmlFor="s-sid" hint="Only you and our admins can see it.">
              <Input id="s-sid" value={form.studentIdNo} onChange={set('studentIdNo')} />
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Gender" htmlFor="s-gender">
                <Select id="s-gender" value={form.gender} onChange={set('gender')}>
                  <option value="">Prefer not to say</option>
                  <option>Female</option>
                  <option>Male</option>
                  <option>Non-binary</option>
                </Select>
              </Field>
              <Field label="Phone" htmlFor="s-phone" hint="Shared only after a session is confirmed.">
                <Input id="s-phone" type="tel" autoComplete="tel" value={form.phone} onChange={set('phone')} />
              </Field>
            </div>
          </div>
        </details>
        <ErrorNote error={act.error} />
        <Button type="submit" className="w-full" size="lg" loading={act.pending}>Create account</Button>
        <p className="text-center text-xs text-ink-500">We'll email you a 6-digit code to confirm your address.</p>
      </form>
    </AuthShell>
  )
}

export function VerifyEmail() {
  useDocumentTitle('Verify your email')
  const { verifyEmail } = useAuth()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const email = params.get('email') ?? ''
  const [code, setCode] = useState('')
  const [resent, setResent] = useState(false)
  const verify = useAction(async () => {
    await verifyEmail(email, code)
    navigate(safeNext(params.get('next')), { replace: true })
  })
  const resend = useAction(async () => {
    await gql(`mutation($e: String!) { resendVerification(email: $e) }`, { e: email })
    setResent(true)
  })
  if (!email) return <Navigate to="/signup" replace />
  return (
    <AuthShell title="Check your inbox" subtitle={<>We sent a 6-digit code to <strong className="text-ink-900">{email}</strong>.</>}>
      <LocalInboxHint />
      <MailCheck className="mb-4 h-10 w-10 text-brand-500" />
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); verify.run() }}>
        <Field label="Verification code" htmlFor="v-code">
          <Input
            id="v-code"
            inputMode="numeric"
            autoComplete="one-time-code"
            pattern="\d{6}"
            maxLength={6}
            required
            autoFocus
            value={code}
            onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
            className="text-center font-mono text-2xl tracking-[0.5em]"
          />
        </Field>
        <ErrorNote error={verify.error || resend.error} />
        {resent && <SuccessNote>A new code is on its way.</SuccessNote>}
        <Button type="submit" className="w-full" size="lg" loading={verify.pending} disabled={code.length !== 6}>Verify email</Button>
        <p className="text-center text-sm text-ink-600">
          No email? Check spam, or{' '}
          <button type="button" className="link" onClick={() => resend.run()} disabled={resend.pending}>send a new code</button>.
        </p>
      </form>
    </AuthShell>
  )
}

export function ForgotPassword() {
  useDocumentTitle('Reset password')
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [email, setEmail] = useState(params.get('email') ?? '')
  const [step, setStep] = useState<'email' | 'code' | 'done'>('email')
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const request = useAction(async () => {
    await gql(`mutation($e: String!) { requestPasswordReset(email: $e) }`, { e: email })
    setStep('code')
  })
  const reset = useAction(async () => {
    await gql(`mutation($e: String!, $c: String!, $p: String!) { resetPassword(email: $e, code: $c, newPassword: $p) }`, { e: email, c: code, p: password })
    setStep('done')
  })
  return (
    <AuthShell title="Reset your password" footer={<Link to="/login" className="link">Back to log in</Link>}>
      {step === 'email' && (
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); request.run() }}>
          <Field label="Email" htmlFor="f-email">
            <Input id="f-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <ErrorNote error={request.error} />
          <Button type="submit" className="w-full" loading={request.pending}>Email me a code</Button>
        </form>
      )}
      {step === 'code' && (
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); reset.run() }}>
          <LocalInboxHint />
          <p className="text-sm text-ink-600">If an account exists for {email}, we've sent it a 6-digit code.</p>
          <Field label="Code" htmlFor="f-code">
            <Input id="f-code" inputMode="numeric" maxLength={6} required value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} className="font-mono tracking-widest" />
          </Field>
          <Field label="New password" htmlFor="f-pass" hint="At least 8 characters. You'll be logged out everywhere else.">
            <Input id="f-pass" type="password" minLength={8} required autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <ErrorNote error={reset.error} />
          <Button type="submit" className="w-full" loading={reset.pending}>Set new password</Button>
        </form>
      )}
      {step === 'done' && (
        <div className="space-y-4">
          <SuccessNote>Your password was changed.</SuccessNote>
          <Button className="w-full" onClick={() => navigate(`/login`)}>Log in</Button>
        </div>
      )}
    </AuthShell>
  )
}
