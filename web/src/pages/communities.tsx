import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  BellOff, BellRing, BookOpen, Building2, ChevronDown, Dumbbell, Globe2, GraduationCap, Heart, Lock, LogOut, MessageCircle,
  Pin, Plus, Search, ShieldCheck, Trash2, Trophy, Users, UsersRound,
} from 'lucide-react'
import { Container } from '../components/Layout'
import { ProviderCard, ReportButton } from '../components/domain'
import { Avatar, Badge, Button, ButtonLink, Empty, ErrorNote, Field, Input, Modal, PageLoader, Pagination, Select, Tabs, Textarea, cx } from '../components/ui'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { gql, GqlError } from '../lib/graphql'
import { useAction, useDebounced, useDocumentTitle, useQuery } from '../lib/hooks'
import { COMMUNITY, COMMUNITY_POST, PROVIDER_CARD } from '../lib/fragments'
import { formatDate, plural, timeAgo } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import { MessageButton } from './messages'
import type { Community, CommunityKind, CommunityPost, CommunityReply, CommunityRole, Page, Provider, User } from '../lib/types'

// Icons are presentation; the list of community types and their names come from the catalog.
const KIND_ICONS: Partial<Record<CommunityKind, typeof Users>> = {
  GENERAL: UsersRound, COURSE: BookOpen, HOSTEL: Building2, CLUB: Trophy, INTEREST: Dumbbell, EXAM: GraduationCap,
}
const kindIcon = (k: string) => KIND_ICONS[k as CommunityKind] ?? UsersRound

function CommunityBadge({ c, size = 44 }: { c: Pick<Community, 'name' | 'kind'>; size?: number }) {
  const K = kindIcon(c.kind)
  return (
    <span style={{ width: size, height: size }} className="flex shrink-0 items-center justify-center rounded-xl bg-brand-100 text-brand-700">
      <K style={{ width: size * 0.46, height: size * 0.46 }} />
    </span>
  )
}

function scopeLabel(c: Community) {
  return c.campus ? c.campus.name : 'All campuses'
}

/** Join, request, or show membership — plus the verify-first prompt. */
function JoinButton({ c, onChange, size = 'md' }: { c: Community; onChange: (c: Community) => void; size?: 'sm' | 'md' }) {
  const { user } = useAuth()
  const toast = useToast()
  const navigate = useNavigate()
  const [verifyOpen, setVerifyOpen] = useState(false)
  const [requestOpen, setRequestOpen] = useState(false)
  const [menu, setMenu] = useState(false)
  const [note, setNote] = useState('')
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setMenu(false)
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [])

  const join = useAction(async (withNote?: string) => {
    try {
      const d = await gql<{ joinCommunity: { status: string; community: Community } }>(
        `mutation($id: ID!, $n: String) { joinCommunity(id: $id, note: $n) { status community { ${COMMUNITY} } } }`,
        { id: c.id, n: withNote ?? null },
      )
      onChange(d.joinCommunity.community)
      setRequestOpen(false)
      toast(d.joinCommunity.status === 'JOINED' ? `You joined ${c.name}` : `Request sent to ${c.name}'s moderators`)
    } catch (e) {
      if (e instanceof GqlError && e.reason === 'VERIFICATION_REQUIRED') {
        setVerifyOpen(true)
        return
      }
      throw e
    }
  })
  const leave = useAction(async () => {
    await gql(`mutation($id: ID!) { leaveCommunity(id: $id) }`, { id: c.id })
    const d = await gql<{ community: Community }>(`query($s: String!) { community(slug: $s) { ${COMMUNITY} } }`, { s: c.slug })
    onChange(d.community)
    setMenu(false)
    toast(c.requestPending ? 'Request withdrawn.' : `You left ${c.name}`)
  })
  const mute = useAction(async () => {
    const d = await gql<{ setCommunityMuted: Community }>(`mutation($id: ID!, $m: Boolean!) { setCommunityMuted(id: $id, muted: $m) { ${COMMUNITY} } }`, { id: c.id, m: !c.muted })
    onChange(d.setCommunityMuted)
    setMenu(false)
    toast(d.setCommunityMuted.muted ? 'Muted: no notifications from this community.' : 'Notifications on.')
  })

  useEffect(() => {
    if (join.error) toast((join.error as Error).message, 'error')
  }, [join.error, toast])

  if (!user) return <ButtonLink to={`/login?next=/communities/${c.slug}`} size={size}>Log in to join</ButtonLink>

  if (c.myRole !== 'NONE') {
    return (
      <div className="relative" ref={ref}>
        <button onClick={() => setMenu((m) => !m)} className={cx('inline-flex items-center gap-1.5 rounded-lg bg-emerald-50 font-semibold text-emerald-800 ring-1 ring-emerald-200 hover:bg-emerald-100', size === 'sm' ? 'px-3 py-1.5 text-sm' : 'px-4 py-2 text-sm')} aria-expanded={menu}>
          {c.myRole === 'OWNER' ? 'Owner' : c.myRole === 'MODERATOR' ? 'Moderator' : 'Joined'} <ChevronDown className="h-4 w-4" />
        </button>
        {menu && (
          <div className="absolute right-0 z-30 mt-1 w-56 overflow-hidden rounded-xl border border-ink-200 bg-white py-1 text-sm shadow-lg">
            <button onClick={() => mute.run()} className="flex w-full items-center gap-2 px-4 py-2 hover:bg-ink-50">
              {c.muted ? <BellRing className="h-4 w-4" /> : <BellOff className="h-4 w-4" />} {c.muted ? 'Turn notifications on' : 'Mute notifications'}
            </button>
            {c.myRole !== 'OWNER' && (
              <button onClick={() => leave.run()} className="flex w-full items-center gap-2 px-4 py-2 text-red-600 hover:bg-red-50">
                <LogOut className="h-4 w-4" /> Leave community
              </button>
            )}
          </div>
        )}
      </div>
    )
  }
  if (c.requestPending) {
    return <Button variant="secondary" size={size} loading={leave.pending} onClick={() => leave.run()} title="Withdraw request">Requested · Withdraw</Button>
  }
  if (c.requestDeclined) {
    return <Button variant="secondary" size={size} disabled>Declined · ask again next week</Button>
  }
  return (
    <>
      {c.joinPolicy === 'OPEN' ? (
        <Button size={size} loading={join.pending} onClick={() => join.run()}>Join</Button>
      ) : (
        <Button size={size} variant="secondary" onClick={() => setRequestOpen(true)}><Lock className="h-4 w-4" /> Request to join</Button>
      )}
      <Modal open={requestOpen} onClose={() => setRequestOpen(false)} title={`Ask to join ${c.name}`}
        footer={<><Button variant="secondary" onClick={() => setRequestOpen(false)}>Cancel</Button><Button loading={join.pending} onClick={() => join.run(note)}>Send request</Button></>}>
        <Field label="A note for the moderators (optional)" hint="Why you'd like to join, in a line.">
          <Textarea value={note} onChange={(e) => setNote(e.target.value)} maxLength={300} rows={3} />
        </Field>
      </Modal>
      <Modal open={verifyOpen} onClose={() => setVerifyOpen(false)} title={`Verify you study at ${user.campus?.name ?? 'your campus'}`}
        footer={<><Button variant="secondary" onClick={() => setVerifyOpen(false)}>Not now</Button><Button onClick={() => navigate('/verify')}>Verify now</Button></>}>
        <p className="text-sm text-ink-700">
          {c.name} is only for verified students of {c.campus?.name ?? 'its campus'}. Use your campus email or upload your student ID — it takes a minute.
        </p>
      </Modal>
    </>
  )
}

function CommunityCard({ c, onChange }: { c: Community; onChange: (c: Community) => void }) {
  return (
    <div className="card flex items-center gap-4 p-4">
      <Link to={`/communities/${c.slug}`} className="flex min-w-0 flex-1 items-center gap-4">
        <CommunityBadge c={c} />
        <div className="min-w-0">
          <p className="truncate font-semibold text-ink-900 hover:text-brand-700">{c.name}</p>
          <p className="flex flex-wrap items-center gap-x-2 text-xs text-ink-500">
            <span className="flex items-center gap-1">{c.campus ? <Building2 className="h-3 w-3" /> : <Globe2 className="h-3 w-3" />} {scopeLabel(c)}</span>
            <span>· {plural(c.memberCount, 'member')}</span>
            {c.joinPolicy === 'APPROVAL' ? <Badge tone="yellow">Approval</Badge> : <Badge tone="green">Open</Badge>}
          </p>
          {c.description && <p className="mt-1 line-clamp-1 text-sm text-ink-600">{c.description}</p>}
        </div>
      </Link>
      <JoinButton c={c} onChange={onChange} size="sm" />
    </div>
  )
}

export function CommunityList() {
  const { communityKinds } = useCatalog()
  useDocumentTitle('Communities')
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') ?? (user ? 'CAMPUS' : 'GLOBAL')) as 'CAMPUS' | 'GLOBAL' | 'JOINED'
  const kind = params.get('kind') ?? ''
  const page = Number(params.get('page') ?? 1)
  const [q, setQ] = useState('')
  const query = useDebounced(q)
  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params)
    if (v) p.set(k, v)
    else p.delete(k)
    if (k !== 'page') p.delete('page')
    setParams(p, { replace: true })
  }
  const { data, loading, error, setData } = useQuery<{ communities: Page<Community> }>(
    `query($t: CommunityTab!, $k: CommunityKind, $q: String, $p: Int) { communities(tab: $t, kind: $k, query: $q, page: $p, pageSize: 20) { total items { ${COMMUNITY} } } }`,
    { t: tab, k: kind || null, q: query || null, p: page },
  )
  const replace = (c: Community) =>
    setData((d) => d && { communities: { ...d.communities, items: d.communities.items.map((x) => (x.id === c.id ? c : x)) } })
  const chip = (on: boolean) => cx('rounded-full border px-3 py-1.5 text-sm font-medium', on ? 'border-ink-900 bg-ink-900 text-white' : 'border-ink-200 bg-white text-ink-700 hover:border-ink-300')

  return (
    <Container className="max-w-4xl">
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold sm:text-3xl">Communities</h1>
          <p className="mt-1 text-ink-600">Your batch, your hostel, your club — join in one tap and talk with people who share it.</p>
        </div>
        {user && <ButtonLink to="/communities/new"><Plus className="h-4 w-4" /> Create community</ButtonLink>}
      </div>
      <Tabs
        value={tab}
        onChange={(v) => set('tab', v)}
        tabs={[
          ...(user ? [{ value: 'CAMPUS' as const, label: user.campus?.name ?? 'My campus' }] : []),
          { value: 'GLOBAL' as const, label: 'All campuses' },
          ...(user ? [{ value: 'JOINED' as const, label: 'Joined' }] : []),
        ]}
      />
      <div className="my-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="relative sm:w-72">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-400" />
          <Input value={q} onChange={(e) => { setQ(e.target.value); if (page > 1) set('page', '') }} placeholder="Search communities" className="pl-9" aria-label="Search communities" />
        </div>
        <div className="flex flex-wrap gap-1.5">
          <button className={chip(!kind)} onClick={() => set('kind', '')}>All types</button>
          {communityKinds.map((k) => <button key={k.value} className={chip(kind === k.value)} onClick={() => set('kind', k.value)}>{k.label}</button>)}
        </div>
      </div>
      <ErrorNote error={error} />
      {loading && !data ? (
        <PageLoader />
      ) : data?.communities.items.length ? (
        <>
          <div className="space-y-3">{data.communities.items.map((c) => <CommunityCard key={c.id} c={c} onChange={replace} />)}</div>
          <Pagination page={page} pageSize={20} total={data.communities.total} onChange={(p) => set('page', String(p))} />
        </>
      ) : (
        <Empty icon={<UsersRound className="h-10 w-10" />} title={tab === 'JOINED' ? "You haven't joined any communities yet" : 'No communities match'}
          action={user ? <ButtonLink to="/communities/new">Create one</ButtonLink> : <ButtonLink to="/signup">Join Your Mate</ButtonLink>}>
          {tab === 'JOINED' ? 'Start with your campus — every campus has an All students community.' : 'Try another type or search, or start your own.'}
        </Empty>
      )}
    </Container>
  )
}

export function NewCommunity() {
  const { communityKinds } = useCatalog()
  useDocumentTitle('Create community')
  const { user } = useAuth()
  const navigate = useNavigate()
  const [form, setForm] = useState({ name: '', description: '', kind: 'CLUB' as CommunityKind, allCampuses: false, joinPolicy: 'OPEN', rules: '' })
  const create = useAction(async () => {
    const d = await gql<{ createCommunity: Community }>(`mutation($i: CommunityInput!) { createCommunity(input: $i) { slug } }`, { i: form })
    navigate(`/communities/${d.createCommunity.slug}`)
  })
  if (!user) return null
  const seg = (on: boolean) => cx('flex-1 rounded-lg border px-3 py-2 text-left text-sm', on ? 'border-brand-500 bg-brand-50 text-brand-900' : 'border-ink-200 text-ink-700 hover:border-ink-300')
  return (
    <Container className="max-w-2xl">
      <h1 className="mb-6 text-2xl font-bold">Create a community</h1>
      {!user.campusVerified && (
        <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
          Verify you study at {user.campus?.name} before creating a community. <Link to="/verify" className="font-semibold underline">Verify now</Link>
        </div>
      )}
      <form className="card space-y-5 p-6" onSubmit={(e: FormEvent) => { e.preventDefault(); create.run() }}>
        <Field label="Name" hint="Make it easy to find, e.g. “CSE Batch 2027” or “Hostel BH-3”.">
          <Input required minLength={3} maxLength={80} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </Field>
        <Field label="What is it?">
          <div className="flex flex-wrap gap-2">
            {communityKinds.map((k) => {
              const Icon = kindIcon(k.value)
              return (
              <button type="button" key={k.value} onClick={() => setForm({ ...form, kind: k.value as CommunityKind })}
                className={cx('inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm font-medium', form.kind === k.value ? 'border-brand-500 bg-brand-500 text-white' : 'border-ink-200 text-ink-700')}>
                <Icon className="h-4 w-4" /> {k.label}
              </button>
              )
            })}
          </div>
        </Field>
        <Field label="Description">
          <Textarea maxLength={500} rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder="Who it's for and what you talk about" />
        </Field>
        <div>
          <p className="label">Who can find and join it</p>
          <div className="flex flex-col gap-2 sm:flex-row">
            <button type="button" className={seg(!form.allCampuses)} onClick={() => setForm({ ...form, allCampuses: false })}>
              <b className="block">{user.campus?.name ?? 'My campus'} only</b><span className="text-xs text-ink-500">Verified students of your campus</span>
            </button>
            <button type="button" className={seg(form.allCampuses)} onClick={() => setForm({ ...form, allCampuses: true })}>
              <b className="block">All campuses</b><span className="text-xs text-ink-500">Students anywhere, e.g. exam prep</span>
            </button>
          </div>
        </div>
        <div>
          <p className="label">How people join</p>
          <div className="flex flex-col gap-2 sm:flex-row">
            <button type="button" className={seg(form.joinPolicy === 'OPEN')} onClick={() => setForm({ ...form, joinPolicy: 'OPEN' })}>
              <b className="block">Open</b><span className="text-xs text-ink-500">Anyone eligible joins in one tap</span>
            </button>
            <button type="button" className={seg(form.joinPolicy === 'APPROVAL')} onClick={() => setForm({ ...form, joinPolicy: 'APPROVAL' })}>
              <b className="block">Approval</b><span className="text-xs text-ink-500">You approve each request; posts are members-only</span>
            </button>
          </div>
        </div>
        <Field label="Rules (optional)">
          <Textarea maxLength={2000} rows={3} value={form.rules} onChange={(e) => setForm({ ...form, rules: e.target.value })} placeholder="Be kind. No selling. Stay on topic." />
        </Field>
        <p className="rounded-lg bg-ink-50 px-3 py-2 text-xs text-ink-600">You'll be the owner and moderator, and you can add moderators later. Each person can own up to 3 communities.</p>
        <ErrorNote error={create.error} />
        <div className="flex gap-2">
          <Button type="submit" loading={create.pending} disabled={!user.campusVerified}>Create community</Button>
          <ButtonLink to="/communities" variant="ghost">Cancel</ButtonLink>
        </div>
      </form>
    </Container>
  )
}

type CommunityTab = 'posts' | 'members' | 'tutors' | 'about' | 'manage'

export function CommunityPage() {
  const { label } = useCatalog()
  const { slug = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const { data, loading, error, setData } = useQuery<{ community: Community | null }>(`query($s: String!) { community(slug: $s) { ${COMMUNITY} owner { id displayName } } }`, { s: slug })
  const c = data?.community
  useDocumentTitle(c?.name ?? 'Community')
  const tab = (params.get('tab') ?? 'posts') as CommunityTab
  if (loading) return <PageLoader />
  if (error) return <Container><ErrorNote error={error} /></Container>
  if (!c) return <Container><Empty title="This community isn't available">It may have been deleted.</Empty></Container>
  const update = (n: Community) => setData(() => ({ community: { ...n, owner: c.owner } }))

  return (
    <Container className="max-w-4xl">
      <Link to="/communities" className="text-sm link">← Communities</Link>
      <section className="card mt-4 overflow-hidden">
        <div className="h-20 bg-gradient-to-r from-brand-500 to-brand-300" />
        <div className="flex flex-col gap-4 px-5 pb-5 sm:flex-row sm:items-end">
          <div className="-mt-8 rounded-2xl bg-white p-1"><CommunityBadge c={c} size={64} /></div>
          <div className="min-w-0 flex-1">
            <h1 className="text-2xl font-bold">{c.name}</h1>
            <p className="flex flex-wrap items-center gap-x-2 text-sm text-ink-600">
              <span>{scopeLabel(c)}</span><span>· {plural(c.memberCount, 'member')}</span><span>· {label('communityKinds', c.kind)}</span>
              {c.joinPolicy === 'APPROVAL' ? <Badge tone="yellow">Approval to join</Badge> : <Badge tone="green">Open</Badge>}
            </p>
          </div>
          <JoinButton c={c} onChange={update} />
        </div>
      </section>
      <div className="mt-6">
        <Tabs
          value={tab}
          onChange={(v) => setParams(v === 'posts' ? {} : { tab: v }, { replace: true })}
          tabs={[
            { value: 'posts', label: 'Posts' },
            { value: 'members', label: 'Members' },
            { value: 'tutors', label: 'Tutors' },
            { value: 'about', label: 'About' },
            ...(c.canModerate ? [{ value: 'manage' as const, label: 'Manage' }] : []),
          ]}
        />
      </div>
      <div className="mt-5">
        {tab === 'posts' && <PostsTab c={c} onChange={update} />}
        {tab === 'members' && <MembersTab c={c} />}
        {tab === 'tutors' && <TutorsTab c={c} />}
        {tab === 'about' && <AboutTab c={c} />}
        {tab === 'manage' && c.canModerate && <ManageTab c={c} onChange={update} />}
      </div>
    </Container>
  )
}

function LockedNote({ c, onChange, what }: { c: Community; onChange: (c: Community) => void; what: string }) {
  return (
    <Empty icon={<Lock className="h-10 w-10" />} title={`Join ${c.name} to see ${what}`} action={<JoinButton c={c} onChange={onChange} />}>
      This community approves each member, so only members can see inside.
    </Empty>
  )
}

function PostsTab({ c, onChange }: { c: Community; onChange: (c: Community) => void }) {
  const { user } = useAuth()
  const [page, setPage] = useState(1)
  const [body, setBody] = useState('')
  const { data, loading, error, refetch, setData } = useQuery<{ communityPosts: Page<CommunityPost> }>(
    `query($c: ID!, $p: Int) { communityPosts(communityId: $c, page: $p, pageSize: 20) { total items { ${COMMUNITY_POST} } } }`,
    { c: c.id, p: page },
    { skip: !user || !c.canSeePosts, pollMs: 30_000 },
  )
  const post = useAction(async () => {
    await gql(`mutation($c: ID!, $b: String!) { createCommunityPost(communityId: $c, body: $b) { id } }`, { c: c.id, b: body })
    setBody('')
    setPage(1)
    refetch()
  })
  if (!user) return <Empty title="Log in to read posts" action={<ButtonLink to={`/login?next=/communities/${c.slug}`}>Log in</ButtonLink>} />
  if (!c.canSeePosts) return <LockedNote c={c} onChange={onChange} what="its posts" />
  const replace = (p: CommunityPost | null, id: string) =>
    setData((d) => d && { communityPosts: { ...d.communityPosts, items: p ? d.communityPosts.items.map((x) => (x.id === id ? p : x)) : d.communityPosts.items.filter((x) => x.id !== id) } })

  return (
    <div className="space-y-4">
      {c.myRole !== 'NONE' ? (
        <form className="card space-y-3 p-4" onSubmit={(e) => { e.preventDefault(); if (body.trim()) post.run() }}>
          <div className="flex gap-3">
            <Avatar name={user.displayName} url={user.avatarUrl} size={36} />
            <Textarea value={body} onChange={(e) => setBody(e.target.value)} maxLength={4000} rows={2} placeholder={`Share with ${c.name}…`} aria-label="New post" />
          </div>
          <ErrorNote error={post.error} />
          <div className="flex justify-end"><Button type="submit" size="sm" loading={post.pending} disabled={!body.trim()}>Post</Button></div>
        </form>
      ) : (
        <div className="card flex flex-col gap-3 p-4 text-sm text-ink-700 sm:flex-row sm:items-center sm:justify-between">
          Join to post, reply and like.
          <JoinButton c={c} onChange={onChange} size="sm" />
        </div>
      )}
      <ErrorNote error={error} />
      {loading && !data ? (
        <PageLoader />
      ) : data?.communityPosts.items.length ? (
        <>
          {data.communityPosts.items.map((p) => <PostItem key={p.id} p={p} c={c} onChange={(n) => replace(n, p.id)} />)}
          <Pagination page={page} pageSize={20} total={data.communityPosts.total} onChange={setPage} />
        </>
      ) : (
        <Empty icon={<MessageCircle className="h-10 w-10" />} title="No posts yet">Start the conversation — ask a question or share something useful.</Empty>
      )}
    </div>
  )
}

function PostItem({ p, c, onChange, linkToPost = true }: { p: CommunityPost; c: Community; onChange: (p: CommunityPost | null) => void; linkToPost?: boolean }) {
  const toast = useToast()
  const like = useAction(async () => {
    const d = await gql<{ likeCommunityPost: CommunityPost }>(`mutation($id: ID!, $l: Boolean!) { likeCommunityPost(id: $id, liked: $l) { ${COMMUNITY_POST} } }`, { id: p.id, l: !p.likedByMe })
    onChange(d.likeCommunityPost)
  })
  const pin = useAction(async () => {
    const d = await gql<{ pinCommunityPost: CommunityPost }>(`mutation($id: ID!, $v: Boolean!) { pinCommunityPost(id: $id, pinned: $v) { ${COMMUNITY_POST} } }`, { id: p.id, v: !p.pinned })
    onChange(d.pinCommunityPost)
  })
  const remove = useAction(async () => {
    await gql(`mutation($id: ID!) { removeCommunityPost(id: $id) }`, { id: p.id })
    onChange(null)
    toast('Post removed.')
  })
  const err = like.error || pin.error || remove.error
  return (
    <article className="card p-4">
      <div className="flex items-center gap-2 text-sm">
        <Avatar name={p.author.displayName} url={p.author.avatarUrl} size={28} />
        <Link to={`/users/${p.author.id}`} className="font-semibold text-ink-900 hover:underline">{p.author.displayName}</Link>
        <span className="text-xs text-ink-400">{timeAgo(p.createdAt)}</span>
        {p.pinned && <Badge tone="brand"><Pin className="h-3 w-3" /> Pinned</Badge>}
      </div>
      <p className="mt-2 whitespace-pre-line break-words text-sm text-ink-800">{p.body}</p>
      <div className="mt-3 flex flex-wrap items-center gap-4 text-sm text-ink-500">
        <button onClick={() => like.run()} disabled={c.myRole === 'NONE'} className={cx('inline-flex items-center gap-1 disabled:opacity-50', p.likedByMe && 'font-semibold text-brand-600')} aria-pressed={p.likedByMe}>
          <Heart className={cx('h-4 w-4', p.likedByMe && 'fill-brand-500 text-brand-500')} /> {p.likeCount}
        </button>
        {linkToPost ? (
          <Link to={`/communities/${c.slug}/posts/${p.id}`} className="inline-flex items-center gap-1 hover:text-ink-800"><MessageCircle className="h-4 w-4" /> {plural(p.replyCount, 'reply', 'replies')}</Link>
        ) : (
          <span className="inline-flex items-center gap-1"><MessageCircle className="h-4 w-4" /> {plural(p.replyCount, 'reply', 'replies')}</span>
        )}
        <span className="ml-auto flex items-center gap-3">
          {c.canModerate && <button onClick={() => pin.run()} className="inline-flex items-center gap-1 text-xs hover:text-ink-800"><Pin className="h-3.5 w-3.5" /> {p.pinned ? 'Unpin' : 'Pin'}</button>}
          {(p.mine || c.canModerate) && <button onClick={() => remove.run()} className="inline-flex items-center gap-1 text-xs hover:text-red-600"><Trash2 className="h-3.5 w-3.5" /> Remove</button>}
          {!p.mine && <ReportButton targetType="COMMUNITY_POST" targetId={p.id} />}
        </span>
      </div>
      <ErrorNote error={err} className="mt-2" />
    </article>
  )
}

export function CommunityPostPage() {
  const { slug = '', postId = '' } = useParams()
  const { user } = useAuth()
  const navigate = useNavigate()
  const [body, setBody] = useState('')
  const { data, loading, error, setData } = useQuery<{ community: Community | null; communityPost: CommunityPost | null }>(
    `query($s: String!, $p: ID!) { community(slug: $s) { ${COMMUNITY} } communityPost(id: $p) { ${COMMUNITY_POST} } }`,
    { s: slug, p: postId },
  )
  const { data: replies, refetch } = useQuery<{ communityReplies: CommunityReply[] }>(
    `query($p: ID!) { communityReplies(postId: $p) { id body createdAt mine author { id displayName avatarUrl } } }`,
    { p: postId },
    { skip: !data?.communityPost, pollMs: 10_000 },
  )
  useDocumentTitle(data?.community?.name ?? 'Post')
  const reply = useAction(async () => {
    await gql(`mutation($p: ID!, $b: String!) { replyToCommunityPost(postId: $p, body: $b) { id } }`, { p: postId, b: body })
    setBody('')
    refetch()
    setData((d) => d && d.communityPost ? { ...d, communityPost: { ...d.communityPost, replyCount: d.communityPost.replyCount + 1 } } : d)
  })
  const removeReply = useAction(async (id: string) => {
    await gql(`mutation($id: ID!) { removeCommunityReply(id: $id) }`, { id })
    refetch()
  })
  if (loading) return <PageLoader />
  if (error) return <Container className="max-w-3xl"><ErrorNote error={error} /></Container>
  const c = data?.community
  const p = data?.communityPost
  if (!c || !p) return <Container className="max-w-3xl"><Empty title="This post isn't available">It may have been removed.</Empty></Container>
  return (
    <Container className="max-w-3xl">
      <Link to={`/communities/${c.slug}`} className="text-sm link">← {c.name}</Link>
      <div className="mt-4">
        <PostItem p={p} c={c} linkToPost={false} onChange={(n) => (n ? setData((d) => d && { ...d, communityPost: n }) : navigate(`/communities/${c.slug}`))} />
      </div>
      <h2 className="mb-3 mt-6 text-sm font-semibold text-ink-600">{plural(replies?.communityReplies.length ?? 0, 'reply', 'replies')}</h2>
      <div className="space-y-3">
        {replies?.communityReplies.map((r) => (
          <div key={r.id} className="card flex gap-3 p-3">
            <Avatar name={r.author.displayName} url={r.author.avatarUrl} size={28} />
            <div className="min-w-0 flex-1">
              <p className="text-sm"><Link to={`/users/${r.author.id}`} className="font-semibold hover:underline">{r.author.displayName}</Link> <span className="text-xs text-ink-400">{timeAgo(r.createdAt)}</span></p>
              <p className="mt-0.5 whitespace-pre-line break-words text-sm text-ink-800">{r.body}</p>
            </div>
            {(r.mine || c.canModerate) && <button onClick={() => removeReply.run(r.id)} className="self-start text-ink-400 hover:text-red-600" aria-label="Remove reply"><Trash2 className="h-4 w-4" /></button>}
          </div>
        ))}
      </div>
      {user && c.myRole !== 'NONE' ? (
        <form className="mt-4 flex gap-2" onSubmit={(e) => { e.preventDefault(); if (body.trim()) reply.run() }}>
          <Input value={body} onChange={(e) => setBody(e.target.value)} maxLength={2000} placeholder="Write a reply" aria-label="Reply" />
          <Button type="submit" loading={reply.pending} disabled={!body.trim()}>Reply</Button>
        </form>
      ) : (
        <p className="mt-4 text-sm text-ink-500">Join {c.name} to reply.</p>
      )}
      <ErrorNote error={reply.error || removeReply.error} className="mt-2" />
    </Container>
  )
}

const ROLE_LABEL: Record<CommunityRole, string> = { OWNER: 'Owner', MODERATOR: 'Moderator', MEMBER: 'Member', NONE: '' }

function MembersTab({ c }: { c: Community }) {
  const { user } = useAuth()
  const toast = useToast()
  const [page, setPage] = useState(1)
  const { data, loading, error, refetch } = useQuery<{ communityMembers: Page<{ role: CommunityRole; joinedAt: string; user: Pick<User, 'id' | 'displayName' | 'avatarUrl' | 'campusVerified'> }> }>(
    `query($c: ID!, $p: Int) { communityMembers(communityId: $c, page: $p, pageSize: 30) { total items { role joinedAt user { id displayName avatarUrl campusVerified } } } }`,
    { c: c.id, p: page },
    { skip: !user || !c.canSeePosts },
  )
  const act = useAction(async (op: 'mod' | 'unmod' | 'remove', userId: string) => {
    if (op === 'remove') await gql(`mutation($c: ID!, $u: ID!) { removeCommunityMember(communityId: $c, userId: $u) }`, { c: c.id, u: userId })
    else await gql(`mutation($c: ID!, $u: ID!, $m: Boolean!) { setCommunityModerator(communityId: $c, userId: $u, moderator: $m) }`, { c: c.id, u: userId, m: op === 'mod' })
    toast(op === 'remove' ? 'Removed. They can’t rejoin for 30 days.' : 'Updated.')
    refetch()
  })
  if (!user) return <Empty title="Log in to see members" />
  if (!c.canSeePosts) return <Empty icon={<Lock className="h-10 w-10" />} title="Only members can see who's here" />
  const isOwner = c.myRole === 'OWNER' || user.isAdmin
  return (
    <>
      <ErrorNote error={error || act.error} className="mb-3" />
      {loading && !data ? <PageLoader /> : (
        <div className="card divide-y divide-ink-100">
          {data?.communityMembers.items.map((m) => (
            <div key={m.user.id} className="flex items-center gap-3 px-4 py-3">
              <Avatar name={m.user.displayName} url={m.user.avatarUrl} size={36} />
              <div className="min-w-0 flex-1">
                <Link to={`/users/${m.user.id}`} className="text-sm font-semibold hover:underline">{m.user.displayName}</Link>
                <p className="text-xs text-ink-500">Joined {formatDate(m.joinedAt)}</p>
              </div>
              {m.role !== 'MEMBER' && <Badge tone={m.role === 'OWNER' ? 'brand' : 'blue'}><ShieldCheck className="h-3 w-3" /> {ROLE_LABEL[m.role]}</Badge>}
              {m.user.id !== user.id && <MessageButton userId={m.user.id} />}
              {m.user.id !== user.id && m.role !== 'OWNER' && (
                <div className="flex gap-1">
                  {isOwner && <Button size="sm" variant="ghost" onClick={() => act.run(m.role === 'MODERATOR' ? 'unmod' : 'mod', m.user.id)}>{m.role === 'MODERATOR' ? 'Remove moderator' : 'Make moderator'}</Button>}
                  {c.canModerate && (m.role === 'MEMBER' || isOwner) && <Button size="sm" variant="ghost" className="text-red-600" onClick={() => act.run('remove', m.user.id)}>Remove</Button>}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
      <Pagination page={page} pageSize={30} total={data?.communityMembers.total ?? 0} onChange={setPage} />
    </>
  )
}

function TutorsTab({ c }: { c: Community }) {
  const { user } = useAuth()
  const { data, loading, error } = useQuery<{ communityTutors: { total: number; items: Provider[] } }>(
    `query($c: ID!) { communityTutors(communityId: $c, pageSize: 24) { total items { ${PROVIDER_CARD} } } }`,
    { c: c.id },
    { skip: !user },
  )
  if (!user) return <Empty title="Log in to see tutors" />
  return (
    <>
      <p className="mb-4 text-sm text-ink-600">Members of {c.name} who tutor, best rated first.</p>
      <ErrorNote error={error} />
      {loading && !data ? <PageLoader /> : data?.communityTutors.items.length ? (
        <div className="grid gap-4 sm:grid-cols-2">{data.communityTutors.items.map((p) => <ProviderCard key={p.id} p={p} />)}</div>
      ) : (
        <Empty icon={<GraduationCap className="h-10 w-10" />} title="No tutors here yet" action={<ButtonLink to="/dashboard/profile" variant="secondary">Become a tutor</ButtonLink>}>
          When members publish a tutor profile, they show up here.
        </Empty>
      )}
    </>
  )
}

function AboutTab({ c }: { c: Community }) {
  return (
    <div className="card space-y-5 p-6 text-sm">
      {c.description && <p className="whitespace-pre-line text-ink-800">{c.description}</p>}
      <dl className="grid gap-4 sm:grid-cols-2">
        <div><dt className="text-ink-500">Who can join</dt><dd className="font-medium">{c.campus ? `Verified students of ${c.campus.name}` : 'Students of any campus'}</dd></div>
        <div><dt className="text-ink-500">How</dt><dd className="font-medium">{c.joinPolicy === 'OPEN' ? 'Open — join in one tap' : 'A moderator approves each request'}</dd></div>
        <div><dt className="text-ink-500">Started</dt><dd className="font-medium">{formatDate(c.createdAt)}{c.owner ? ` by ${c.owner.displayName}` : ''}</dd></div>
        <div><dt className="text-ink-500">Activity</dt><dd className="font-medium">{plural(c.memberCount, 'member')} · {plural(c.postCount, 'post')}</dd></div>
      </dl>
      {c.rules && (
        <div>
          <h3 className="font-semibold">Rules</h3>
          <p className="mt-1 whitespace-pre-line text-ink-700">{c.rules}</p>
        </div>
      )}
      <div className="border-t border-ink-100 pt-4"><ReportButton targetType="COMMUNITY" targetId={c.id} label="Report this community" /></div>
    </div>
  )
}

interface CommunityReport {
  id: string
  reason: string
  details: string
  createdAt: string
  targetId: string
  targetLabel: string
  targetLink: string | null
}

function ManageTab({ c, onChange }: { c: Community; onChange: (c: Community) => void }) {
  const { communityKinds } = useCatalog()
  const { user } = useAuth()
  const toast = useToast()
  const navigate = useNavigate()
  const { data, refetch } = useQuery<{ communityJoinRequests: { id: string; note: string; createdAt: string; user: Pick<User, 'id' | 'displayName' | 'avatarUrl'> }[]; communityReports: CommunityReport[] }>(
    `query($c: ID!) { communityJoinRequests(communityId: $c) { id note createdAt user { id displayName avatarUrl } }
      communityReports(communityId: $c) { id reason details createdAt targetId targetLabel targetLink } }`,
    { c: c.id },
  )
  const review = useAction(async (id: string, approve: boolean) => {
    await gql(`mutation($id: ID!, $a: Boolean!) { reviewJoinRequest(id: $id, approve: $a) }`, { id, a: approve })
    toast(approve ? 'Approved.' : 'Declined.')
    refetch()
    const d = await gql<{ community: Community }>(`query($s: String!) { community(slug: $s) { ${COMMUNITY} } }`, { s: c.slug })
    onChange(d.community)
  })
  const moderate = useAction(async (id: string, removePost: boolean) => {
    await gql(`mutation($id: ID!, $r: Boolean!) { moderateCommunityReport(id: $id, removePost: $r) }`, { id, r: removePost })
    toast(removePost ? 'Post removed.' : 'Report dismissed.')
    refetch()
  })
  const [form, setForm] = useState({ name: c.name, description: c.description, kind: c.kind, joinPolicy: c.joinPolicy, rules: c.rules })
  const save = useAction(async () => {
    const d = await gql<{ updateCommunity: Community }>(`mutation($id: ID!, $i: CommunityUpdateInput!) { updateCommunity(id: $id, input: $i) { ${COMMUNITY} } }`, { id: c.id, i: form })
    onChange(d.updateCommunity)
    toast('Settings saved.')
  })
  const [confirmDelete, setConfirmDelete] = useState(false)
  const del = useAction(async () => {
    await gql(`mutation($id: ID!) { deleteCommunity(id: $id) }`, { id: c.id })
    toast('Community deleted.')
    navigate('/communities')
  })
  const isOwner = c.myRole === 'OWNER' || !!user?.isAdmin

  return (
    <div className="space-y-6">
      <ErrorNote error={review.error || moderate.error} />
      <section className="card">
        <h2 className="border-b border-ink-100 px-5 py-3 font-semibold">Join requests {data && `(${data.communityJoinRequests.length})`}</h2>
        {c.joinPolicy === 'OPEN' && !data?.communityJoinRequests.length ? (
          <p className="px-5 py-4 text-sm text-ink-500">This community is open, so people join without asking.</p>
        ) : data?.communityJoinRequests.length ? (
          <div className="divide-y divide-ink-100">
            {data.communityJoinRequests.map((r) => (
              <div key={r.id} className="flex flex-col gap-2 px-5 py-3 sm:flex-row sm:items-center">
                <Avatar name={r.user.displayName} url={r.user.avatarUrl} size={34} />
                <div className="min-w-0 flex-1 text-sm">
                  <Link to={`/users/${r.user.id}`} className="font-semibold hover:underline">{r.user.displayName}</Link> <span className="text-xs text-ink-400">{timeAgo(r.createdAt)}</span>
                  {r.note && <p className="text-ink-600">“{r.note}”</p>}
                </div>
                <div className="flex gap-2">
                  <Button size="sm" onClick={() => review.run(r.id, true)}>Approve</Button>
                  <Button size="sm" variant="secondary" onClick={() => review.run(r.id, false)}>Decline</Button>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="px-5 py-4 text-sm text-ink-500">No requests waiting.</p>
        )}
      </section>

      <section className="card">
        <h2 className="border-b border-ink-100 px-5 py-3 font-semibold">Reported posts {data && `(${data.communityReports.length})`}</h2>
        {data?.communityReports.length ? (
          <div className="divide-y divide-ink-100">
            {data.communityReports.map((r) => (
              <div key={r.id} className="space-y-2 px-5 py-3 text-sm">
                <div className="flex items-center gap-2"><Badge tone="red">{r.reason.replace(/_/g, ' ').toLowerCase()}</Badge><span className="text-xs text-ink-400">{timeAgo(r.createdAt)}</span></div>
                <p className="text-ink-800">{r.targetLink ? <Link to={r.targetLink} className="link">{r.targetLabel}</Link> : r.targetLabel}</p>
                {r.details && <p className="text-ink-600">{r.details}</p>}
                <div className="flex gap-2">
                  <Button size="sm" variant="danger" onClick={() => moderate.run(r.id, true)}>Remove post</Button>
                  <Button size="sm" variant="secondary" onClick={() => moderate.run(r.id, false)}>Dismiss</Button>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="px-5 py-4 text-sm text-ink-500">Nothing reported. Members can report any post; reports also reach the Your Mate team.</p>
        )}
      </section>

      {isOwner && (
        <section className="card p-5">
          <h2 className="font-semibold">Settings</h2>
          <form className="mt-4 space-y-4" onSubmit={(e) => { e.preventDefault(); save.run() }}>
            <Field label="Name"><Input value={form.name} maxLength={80} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Type">
                <Select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as CommunityKind })}>{communityKinds.map((k) => <option key={k.value} value={k.value}>{k.label}</option>)}</Select>
              </Field>
              <Field label="How people join">
                <Select value={form.joinPolicy} onChange={(e) => setForm({ ...form, joinPolicy: e.target.value as Community['joinPolicy'] })}><option value="OPEN">Open</option><option value="APPROVAL">Approval</option></Select>
              </Field>
            </div>
            <Field label="Description"><Textarea value={form.description} maxLength={500} rows={3} onChange={(e) => setForm({ ...form, description: e.target.value })} /></Field>
            <Field label="Rules"><Textarea value={form.rules} maxLength={2000} rows={3} onChange={(e) => setForm({ ...form, rules: e.target.value })} /></Field>
            <ErrorNote error={save.error} />
            <div className="flex flex-wrap gap-2">
              <Button type="submit" loading={save.pending}>Save settings</Button>
              {!c.isDefault && <Button type="button" variant="ghost" className="text-red-600" onClick={() => setConfirmDelete(true)}>Delete community</Button>}
            </div>
          </form>
        </section>
      )}
      <Modal open={confirmDelete} onClose={() => setConfirmDelete(false)} title={`Delete ${c.name}?`}
        footer={<><Button variant="secondary" onClick={() => setConfirmDelete(false)}>Keep it</Button><Button variant="danger" loading={del.pending} onClick={() => del.run()}>Delete</Button></>}>
        <p className="text-sm text-ink-700">Members lose access to its posts. This can't be undone.</p>
        <ErrorNote error={del.error} className="mt-3" />
      </Modal>
    </div>
  )
}

export function JoinedCommunitiesCard() {
  const { data } = useQuery<{ communities: Page<Community> }>(`{ communities(tab: JOINED, pageSize: 5) { total items { ${COMMUNITY} } } }`)
  const items = data?.communities.items ?? []
  return (
    <section className="card p-5">
      <div className="flex items-center justify-between">
        <h2 className="font-semibold">Your communities</h2>
        <Link to="/communities" className="text-sm link">Browse</Link>
      </div>
      {items.length ? (
        <ul className="mt-3 space-y-2">
          {items.map((c) => (
            <li key={c.id}>
              <Link to={`/communities/${c.slug}`} className="flex items-center gap-3 rounded-lg p-1 hover:bg-ink-50">
                <CommunityBadge c={c} size={32} />
                <span className="min-w-0"><span className="block truncate text-sm font-medium">{c.name}</span><span className="text-xs text-ink-500">{plural(c.memberCount, 'member')}</span></span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <p className="mt-2 text-sm text-ink-600">Join your campus's All students community to meet people. <Link to="/communities" className="link">Find communities</Link></p>
      )}
    </section>
  )
}
