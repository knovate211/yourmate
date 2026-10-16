import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { ArrowRight, BadgeCheck, BookOpen, CalendarCheck, Compass, Globe2, GraduationCap, Search, ShieldCheck, Star, Wallet } from 'lucide-react'
import { Container } from '../components/Layout'
import { CampusPicker } from '../components/pickers'
import { ProviderCard } from '../components/domain'
import { Button, ButtonLink, Empty, ErrorNote, Field, Input, PageHeader, PageLoader, Pagination, SuccessNote, Textarea, cx } from '../components/ui'
import { useAuth } from '../lib/auth'
import { gql } from '../lib/graphql'
import { useAction, useDebounced, useDocumentTitle, useQuery } from '../lib/hooks'
import { CAMPUS, PROVIDER_CARD } from '../lib/fragments'
import { browserTimeZone, plural } from '../lib/format'
import type { Campus, CountryCount, Provider } from '../lib/types'

export function Home() {
  useDocumentTitle('')
  const { user } = useAuth()
  const navigate = useNavigate()
  const [campus, setCampus] = useState<Campus | null>(null)
  const [q, setQ] = useState('')
  const campusId = campus?.id ?? user?.campus?.id
  const { data } = useQuery<{ providers: { items: Provider[] }; campusCountries: CountryCount[] }>(
    `query($c: ID) { providers(filter: {kind: TUTOR, campusId: $c, sort: RATING}, pageSize: 6) { items { ${PROVIDER_CARD} } } campusCountries { country campuses } }`,
    { c: campusId ?? null },
  )
  const countries = data?.campusCountries.length ?? 0
  const campusTotal = data?.campusCountries.reduce((n, c) => n + c.campuses, 0) ?? 0

  const search = (e: FormEvent) => {
    e.preventDefault()
    const p = new URLSearchParams()
    if (q) p.set('q', q)
    if (campus) p.set('campus', campus.id)
    navigate(`/tutors?${p}`)
  }

  return (
    <>
      <section className="relative overflow-hidden bg-gradient-to-b from-brand-50 via-white to-ink-50">
        <div className="mx-auto grid max-w-6xl items-center gap-10 px-4 py-14 md:grid-cols-[1.1fr_1fr] md:py-20">
          <div>
            <span className="inline-flex items-center gap-1.5 rounded-full border border-brand-200 bg-white px-3 py-1 text-xs font-semibold text-brand-700">
              <Globe2 className="h-3.5 w-3.5" /> {data ? `${campusTotal} campuses in ${plural(countries, 'country', 'countries')}` : 'Campuses worldwide'}
            </span>
            <h1 className="mt-4 text-4xl font-extrabold leading-tight text-ink-900 sm:text-5xl">
              Learn from seniors who've <span className="text-brand-500">been there</span>. Earn by helping.
            </h1>
            <p className="mt-4 max-w-xl text-lg text-ink-600">
              Your Mate connects you with verified students on your own campus: tutors who took your exact courses, and guides who know where everything is.
            </p>
            {/* Campus on its own row so the subject field keeps a usable width;
                every control shares one height so the edges line up. */}
            <form onSubmit={search} className="mt-8 grid gap-2 rounded-2xl border border-ink-200 bg-white p-2 shadow-sm">
              <CampusPicker value={campus} onChange={setCampus} placeholder={user?.campus?.name ?? 'Your campus'} className="h-11" />
              <div className="flex flex-col gap-2 sm:flex-row">
                <div className="relative min-w-0 flex-1">
                  <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-400" />
                  <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Subject or course code" className="h-11 pl-9" aria-label="Subject" />
                </div>
                <Button type="submit" className="h-11 shrink-0 px-6">Find a tutor</Button>
              </div>
            </form>
            <p className="mt-3 text-sm text-ink-500">
              New to campus?{' '}
              <Link to="/guides" className="link">Find a campus guide</Link>
            </p>
          </div>
          <div className="relative hidden md:block">
            <div className="absolute -right-10 -top-10 h-72 w-72 rounded-full bg-brand-200/40 blur-3xl" />
            <div className="relative grid gap-4">
              {[
                { icon: GraduationCap, title: 'Tutors', text: 'Seniors who aced the course you are taking now.' },
                { icon: Compass, title: 'Campus guides', text: 'Tours, hostels, paperwork and the city, from someone who lives it.' },
                { icon: ShieldCheck, title: 'Verified students only', text: 'Campus email or student ID checked before anyone can book or teach.' },
              ].map((f, i) => (
                <div key={f.title} className="card flex items-start gap-4 p-5 shadow-sm" style={{ marginLeft: i === 1 ? 40 : 0 }}>
                  <span className="rounded-xl bg-brand-100 p-2.5 text-brand-600">
                    <f.icon className="h-5 w-5" />
                  </span>
                  <div>
                    <h3 className="font-semibold">{f.title}</h3>
                    <p className="text-sm text-ink-600">{f.text}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      <Container className="py-14">
        <h2 className="text-center text-2xl font-bold sm:text-3xl">How it works</h2>
        <div className="mt-8 grid gap-6 md:grid-cols-3">
          {[
            { icon: BadgeCheck, title: '1. Verify you’re a student', text: 'Sign up with your campus email, or upload your student ID. Every account is tied to a real campus.' },
            { icon: CalendarCheck, title: '2. Book a time that suits you', text: 'Pick a tutor or guide, choose an open slot in their calendar and send a request. They accept or suggest another time.' },
            { icon: Star, title: '3. Meet, learn, review', text: 'Meet online or in person, pay the agreed price directly, then leave a review to help the next student.' },
          ].map((s) => (
            <div key={s.title} className="card p-6">
              <s.icon className="h-6 w-6 text-brand-500" />
              <h3 className="mt-3 font-semibold">{s.title}</h3>
              <p className="mt-1 text-sm text-ink-600">{s.text}</p>
            </div>
          ))}
        </div>
      </Container>

      {data && data.providers.items.length > 0 && (
        <Container className="py-4">
          <div className="mb-5 flex items-end justify-between">
            <h2 className="text-2xl font-bold">Top tutors{campusId ? ' on your campus' : ''}</h2>
            <Link to={`/tutors${campusId ? `?campus=${campusId}` : ''}`} className="link flex items-center gap-1 text-sm">
              See all <ArrowRight className="h-4 w-4" />
            </Link>
          </div>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {data.providers.items.map((p) => <ProviderCard key={p.id} p={p} />)}
          </div>
        </Container>
      )}

      <Container className="py-14">
        <div className="grid gap-6 rounded-3xl bg-ink-900 p-8 text-white md:grid-cols-[1.4fr_1fr] md:p-12">
          <div>
            <Wallet className="h-8 w-8 text-brand-400" />
            <h2 className="mt-4 text-3xl font-bold">Good at something? Earn from it.</h2>
            <p className="mt-3 max-w-lg text-ink-300">
              Set your own rate and hours, teach the subjects you know best or show newcomers around. You decide which requests to accept.
            </p>
          </div>
          <div className="flex flex-col justify-center gap-3">
            <ButtonLink to={user ? '/dashboard/profile' : '/signup?intent=teach'} size="lg">
              <BookOpen className="h-5 w-5" /> Become a tutor
            </ButtonLink>
            <ButtonLink to={user ? '/dashboard/profile?kind=GUIDE' : '/signup?intent=guide'} size="lg" variant="secondary">
              <Compass className="h-5 w-5" /> Become a campus guide
            </ButtonLink>
          </div>
        </div>
      </Container>
    </>
  )
}

export function About() {
  useDocumentTitle('About us')
  return (
    <Container>
      <PageHeader title="About Your Mate" subtitle="A companion for people who want to learn or earn in student life." />
      <div className="card space-y-4 p-6 text-ink-700 sm:p-8">
        <p>
          Your Mate is built on a simple observation: the best person to help you through a course is often the senior who took it last year, and the best person to show you around a new campus is someone who already lives there.
        </p>
        <p>
          Our vision is a new world of learning where students become lifelong learners and help each other build 21st-century skills — a world of creativity, inspiration and ambition informed by experience. Today Your Mate is open to students on any campus, anywhere: every campus has its own verified community, its own subjects and its own feed.
        </p>
        <p>
          Trust comes first. Everyone is verified as a student of their campus, providers are ID-checked by our team, and every session can be reviewed and reported.
        </p>
      </div>
    </Container>
  )
}

export function Contact() {
  useDocumentTitle('Contact')
  const { user } = useAuth()
  const [form, setForm] = useState({ name: user ? `${user.firstName} ${user.lastName ?? ''}`.trim() : '', email: user?.email ?? '', message: '' })
  const [sent, setSent] = useState(false)
  const send = useAction(async () => {
    await gql(`mutation($i: ContactInput!) { contact(input: $i) }`, { i: form })
    setSent(true)
  })
  return (
    <Container className="max-w-3xl">
      <PageHeader title="Contact us" subtitle="Questions, partnerships, or a problem with a session? We read every message." />
      <div className="card p-6">
        {sent ? (
          <SuccessNote>Thanks, {form.name.split(' ')[0]}! We'll reply to {form.email} soon.</SuccessNote>
        ) : (
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault()
              send.run()
            }}
          >
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Full name" htmlFor="c-name">
                <Input id="c-name" required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              </Field>
              <Field label="Email" htmlFor="c-email">
                <Input id="c-email" type="email" required value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />
              </Field>
            </div>
            <Field label="Message" htmlFor="c-msg">
              <Textarea id="c-msg" required minLength={10} rows={6} value={form.message} onChange={(e) => setForm({ ...form, message: e.target.value })} />
            </Field>
            <ErrorNote error={send.error} />
            <Button type="submit" loading={send.pending}>Send message</Button>
          </form>
        )}
      </div>
    </Container>
  )
}

export function Campuses() {
  useDocumentTitle('Campuses')
  const [params, setParams] = useSearchParams()
  const [q, setQ] = useState(params.get('q') ?? '')
  const query = useDebounced(q)
  const country = params.get('country') ?? ''
  const page = Number(params.get('page') ?? 1)
  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params)
    if (v) p.set(k, v)
    else p.delete(k)
    if (k !== 'page') p.delete('page')
    setParams(p, { replace: true })
  }
  const { data: counts } = useQuery<{ campusCountries: CountryCount[] }>(`{ campusCountries { country campuses } }`)
  const { data, loading, error } = useQuery<{ campusDirectory: { total: number; items: Campus[] } }>(
    `query($q: String, $c: String, $p: Int) { campusDirectory(query: $q, country: $c, page: $p, pageSize: 60) { total items { ${CAMPUS} memberCount } } }`,
    { q: query || null, c: country || null, p: page },
  )
  const all = counts?.campusCountries.reduce((n, c) => n + c.campuses, 0) ?? 0
  const chip = (active: boolean) =>
    cx('shrink-0 rounded-full border px-3 py-1.5 text-sm font-medium', active ? 'border-ink-900 bg-ink-900 text-white' : 'border-ink-200 bg-white text-ink-700 hover:border-ink-300')
  return (
    <Container>
      <PageHeader title="Campuses on Your Mate" subtitle="Every campus has its own verified community." actions={<ButtonLink to="/campuses/request" variant="secondary">Add your campus</ButtonLink>} />
      <div className="mb-4 flex gap-2 overflow-x-auto pb-1">
        <button className={chip(!country)} onClick={() => set('country', '')}>All ({all})</button>
        {counts?.campusCountries.map((c) => (
          <button key={c.country} className={chip(country === c.country)} onClick={() => set('country', c.country)}>
            {c.country} ({c.campuses})
          </button>
        ))}
      </div>
      <Input
        value={q}
        onChange={(e) => { setQ(e.target.value); if (page > 1) set('page', '') }}
        placeholder="Search by name, initials, city or email domain"
        className="mb-6 max-w-md"
        aria-label="Search campuses"
      />
      <ErrorNote error={error} />
      {loading && !data ? (
        <PageLoader />
      ) : data?.campusDirectory.items.length ? (
        <>
          <p className="mb-3 text-sm text-ink-500">{plural(data.campusDirectory.total, 'campus', 'campuses')}</p>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {data.campusDirectory.items.map((c) => (
              <Link key={c.id} to={`/tutors?campus=${c.id}`} className="card p-4 transition hover:border-brand-300">
                <p className="font-semibold">{c.name}</p>
                <p className="text-sm text-ink-500">
                  {[c.city, c.country].filter(Boolean).join(', ')} · {c.currency}
                  {c.memberCount ? ` · ${plural(c.memberCount, 'member')}` : ''}
                </p>
              </Link>
            ))}
          </div>
          <Pagination page={page} pageSize={60} total={data.campusDirectory.total} onChange={(p) => set('page', String(p))} />
        </>
      ) : (
        <Empty title="No campus matches" action={<ButtonLink to="/campuses/request">Add your campus</ButtonLink>}>
          Try its initials or another spelling, or add it — it takes a minute.
        </Empty>
      )}
    </Container>
  )
}

// Every ISO currency the browser knows, so no list is kept here.
const CURRENCIES: string[] = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('currency') ?? []

export function RequestCampus() {
  useDocumentTitle('Add your campus')
  const navigate = useNavigate()
  const [form, setForm] = useState({ name: '', country: '', city: '', timezone: browserTimeZone(), currency: '', domains: '' })
  const [done, setDone] = useState<Campus | null>(null)
  const zones = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') ?? [form.timezone]
  const send = useAction(async () => {
    const d = await gql<{ requestCampus: Campus }>(`mutation($i: RequestCampusInput!) { requestCampus(input: $i) { ${CAMPUS} } }`, {
      i: {
        name: form.name,
        country: form.country,
        city: form.city,
        timezone: form.timezone,
        currency: form.currency,
        emailDomains: form.domains.split(/[\s,]+/).filter(Boolean),
      },
    })
    setDone(d.requestCampus)
  })
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value })
  const typedName = useDebounced(form.name.trim(), 300)
  const { data: similar } = useQuery<{ campuses: Campus[] }>(`query($q: String) { campuses(query: $q, limit: 5) { ${CAMPUS} } }`, { q: typedName }, { skip: typedName.length < 3 })
  const { data: countries } = useQuery<{ campusCountries: CountryCount[] }>(`{ campusCountries { country campuses } }`)

  if (done) {
    return (
      <Container className="max-w-2xl">
        <div className="card p-8 text-center">
          <BadgeCheck className="mx-auto h-10 w-10 text-brand-500" />
          <h1 className="mt-3 text-2xl font-bold">{done.name} was submitted</h1>
          <p className="mt-2 text-ink-600">
            Our team checks the details and the email domains, usually within a day. You can already sign up and choose it as your campus; you'll be fully verified once it's approved.
          </p>
          <Button className="mt-6" onClick={() => navigate(`/signup?campus=${done.id}`)}>Sign up with this campus</Button>
        </div>
      </Container>
    )
  }

  return (
    <Container className="max-w-2xl">
      <PageHeader title="Add your campus" subtitle="Your Mate works at any university or college. Tell us about yours." />
      <form className="card space-y-4 p-6" onSubmit={(e) => { e.preventDefault(); send.run() }}>
        <Field label="Official name" htmlFor="rc-name">
          <Input id="rc-name" required minLength={3} value={form.name} onChange={set('name')} placeholder="Your university or college's official name" />
        </Field>
        {typedName.length >= 3 && similar && similar.campuses.length > 0 && (
          <div className="rounded-lg border border-sky-200 bg-sky-50 p-3 text-sm text-sky-900">
            <p className="font-medium">Is it one of these? They're already on Your Mate:</p>
            <ul className="mt-1.5 space-y-1">
              {similar.campuses.map((c) => (
                <li key={c.id}>
                  <Link to={`/signup?campus=${c.id}`} className="link">{c.name}</Link>
                  <span className="text-sky-800"> · {[c.city, c.country].filter(Boolean).join(', ')}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Country" htmlFor="rc-country">
            <Input id="rc-country" required list="rc-countries" value={form.country} onChange={set('country')} />
            <datalist id="rc-countries">{countries?.campusCountries.map((c) => <option key={c.country} value={c.country} />)}</datalist>
          </Field>
          <Field label="City" htmlFor="rc-city">
            <Input id="rc-city" value={form.city} onChange={set('city')} />
          </Field>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Timezone" htmlFor="rc-tz" hint="Session times on this campus use this timezone.">
            <select id="rc-tz" className="input" value={form.timezone} onChange={set('timezone')}>
              {zones.map((z) => <option key={z}>{z}</option>)}
            </select>
          </Field>
          <Field label="Currency" htmlFor="rc-cur" hint="Tutors on this campus set prices in it.">
            <select id="rc-cur" className="input" required value={form.currency} onChange={set('currency')}>
              <option value="" disabled>Choose…</option>
              {CURRENCIES.map((c) => <option key={c}>{c}</option>)}
            </select>
          </Field>
        </div>
        <Field label="Student email domains" htmlFor="rc-dom" hint="The part after @ in student email addresses. Separate several with commas. Leave empty if students don't get campus email; they'll verify with an ID card instead.">
          <Input id="rc-dom" value={form.domains} onChange={set('domains')} placeholder="students.example.edu" />
        </Field>
        <ErrorNote error={send.error} />
        <Button type="submit" loading={send.pending}>Submit campus</Button>
      </form>
    </Container>
  )
}

export function NotFound() {
  useDocumentTitle('Not found')
  return (
    <Container className="py-24 text-center">
      <p className="text-sm font-semibold text-brand-600">404</p>
      <h1 className="mt-2 text-3xl font-bold">This page doesn't exist</h1>
      <p className="mt-2 text-ink-600">The link may be old, or the page may have moved.</p>
      <ButtonLink to="/" className="mt-6">Go home</ButtonLink>
    </Container>
  )
}
