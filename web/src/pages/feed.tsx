import { useState } from 'react'
import { CalendarDays, ImagePlus, Lightbulb, MapPin, Megaphone, Newspaper, Trash2 } from 'lucide-react'
import { Container } from '../components/Layout'
import { CampusPicker } from '../components/pickers'
import { ReportButton, VerificationBanner } from '../components/domain'
import { Avatar, Badge, Button, Empty, ErrorNote, Field, Input, Modal, PageHeader, PageLoader, Pagination, Select, Textarea, cx } from '../components/ui'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { gql, gqlUpload } from '../lib/graphql'
import { useAction, useDocumentTitle, useQuery } from '../lib/hooks'
import { POST } from '../lib/fragments'
import { formatDateTime, timeAgo } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import type { Campus, Post, PostType } from '../lib/types'

// Icons are presentation; the list of post types and their names come from the catalog.
const TYPE_ICONS: Partial<Record<PostType, typeof Megaphone>> = { EVENT: CalendarDays, NOTICE: Megaphone, PLACE: MapPin, TIP: Lightbulb }
const typeIcon = (t: string) => TYPE_ICONS[t as PostType] ?? Megaphone

export function PostCard({ p, onDelete }: { p: Post; onDelete?: () => void }) {
  const { label } = useCatalog()
  const Icon = typeIcon(p.type)
  return (
    <article className="card overflow-hidden">
      {p.imageUrl && <img src={p.imageUrl} alt="" className="max-h-80 w-full object-cover" loading="lazy" />}
      <div className="p-5">
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Badge tone="brand"><Icon className="h-3 w-3" /> {label('postTypes', p.type)}</Badge>
          {p.status !== 'PUBLISHED' && <Badge tone={p.status === 'PENDING' ? 'yellow' : 'red'}>{p.status === 'PENDING' ? 'Waiting for review' : 'Not approved'}</Badge>}
          <span className="text-ink-500">{p.campus?.name}</span>
        </div>
        <h3 className="mt-2 text-lg font-semibold">{p.title}</h3>
        {(p.eventAt || p.location) && (
          <p className="mt-1 flex flex-wrap gap-x-3 text-sm text-ink-600">
            {p.eventAt && <span className="flex items-center gap-1"><CalendarDays className="h-4 w-4" /> {formatDateTime(p.eventAt)}</span>}
            {p.location && <span className="flex items-center gap-1"><MapPin className="h-4 w-4" /> {p.location}</span>}
          </p>
        )}
        <p className="mt-2 whitespace-pre-line text-sm text-ink-700">{p.body}</p>
        {p.status === 'REJECTED' && p.reviewReason && <p className="mt-2 text-xs text-red-700">Reviewer note: {p.reviewReason}</p>}
        <div className="mt-4 flex items-center justify-between text-xs text-ink-500">
          <span className="flex items-center gap-2"><Avatar name={p.author.displayName} url={p.author.avatarUrl} size={22} /> {p.author.displayName} · {timeAgo(p.createdAt)}</span>
          {p.mine && onDelete ? (
            <button onClick={onDelete} className="flex items-center gap-1 hover:text-red-600"><Trash2 className="h-3.5 w-3.5" /> Delete</button>
          ) : (
            !p.mine && <ReportButton targetType="POST" targetId={p.id} />
          )}
        </div>
      </div>
    </article>
  )
}

export function Feed() {
  const { postTypes } = useCatalog()
  useDocumentTitle('Campus feed')
  const { user } = useAuth()
  const [campus, setCampus] = useState<Campus | null | undefined>(undefined)
  const [type, setType] = useState<PostType | ''>('')
  const [page, setPage] = useState(1)
  const [composing, setComposing] = useState(false)
  const campusId = campus === undefined ? (user?.campus?.id ?? null) : (campus?.id ?? null)
  const { data, loading, refetch } = useQuery<{ posts: { total: number; items: Post[] } }>(
    `query($c: ID, $t: PostType, $p: Int) { posts(campusId: $c, type: $t, page: $p, pageSize: 20) { total items { ${POST} } } }`,
    { c: campusId, t: type || null, p: page },
  )
  const { data: mine, refetch: refetchMine } = useQuery<{ myPosts: { items: Post[] } }>(`{ myPosts { items { ${POST} } } }`, undefined, { skip: !user })
  const pending = mine?.myPosts.items.filter((p) => p.status !== 'PUBLISHED') ?? []
  const remove = useAction(async (id: string) => {
    await gql(`mutation($id: ID!) { deletePost(id: $id) }`, { id })
    refetch()
    refetchMine()
  })

  return (
    <Container className="max-w-3xl">
      <PageHeader
        title="Campus feed"
        subtitle="Events, notices, places worth knowing and tips — shared by verified students, checked by our team."
        actions={user?.campusVerified && <Button onClick={() => setComposing(true)}>Share something</Button>}
      />
      <div className="mb-5 flex flex-col gap-3 sm:flex-row">
        <div className="sm:w-80">
          <CampusPicker value={campus === undefined ? (user?.campus ?? null) : campus} allowAll onChange={(c) => { setCampus(c); setPage(1) }} />
        </div>
        <div className="flex flex-wrap gap-1.5">
          {[{ value: '', label: 'All' }, ...postTypes].map((t) => (
            <button key={t.value} onClick={() => { setType(t.value as PostType | ''); setPage(1) }} className={cx('rounded-full border px-3 py-1.5 text-sm font-medium', type === t.value ? 'border-ink-900 bg-ink-900 text-white' : 'border-ink-200 bg-white text-ink-700')}>
              {t.label}
            </button>
          ))}
        </div>
      </div>
      {user && !user.campusVerified && <div className="mb-5"><VerificationBanner user={user} need="campus" /></div>}
      {pending.length > 0 && (
        <div className="mb-6 space-y-3">
          <p className="text-sm font-semibold text-ink-700">Your posts in review</p>
          {pending.map((p) => <PostCard key={p.id} p={p} onDelete={() => remove.run(p.id)} />)}
        </div>
      )}
      {loading && !data ? (
        <PageLoader />
      ) : data?.posts.items.length ? (
        <div className="space-y-4">
          {data.posts.items.map((p) => <PostCard key={p.id} p={p} onDelete={() => remove.run(p.id)} />)}
          <Pagination page={page} pageSize={20} total={data.posts.total} onChange={setPage} />
        </div>
      ) : (
        <Empty icon={<Newspaper className="h-10 w-10" />} title="Nothing here yet">Be the first to share an event or a tip for your campus.</Empty>
      )}
      <Composer open={composing} onClose={() => setComposing(false)} onPosted={() => { setComposing(false); refetchMine() }} />
    </Container>
  )
}

function Composer({ open, onClose, onPosted }: { open: boolean; onClose: () => void; onPosted: () => void }) {
  const { postTypes } = useCatalog()
  const toast = useToast()
  const [form, setForm] = useState({ type: 'EVENT' as PostType, title: '', body: '', location: '', eventAt: '' })
  const [image, setImage] = useState<File | null>(null)
  const post = useAction(async () => {
    const input = { type: form.type, title: form.title, body: form.body, location: form.location, eventAt: form.eventAt ? new Date(form.eventAt).toISOString() : null }
    const q = `mutation($i: PostInput!) { createPost(input: $i) { id } }`
    if (image) await gqlUpload(q, { i: input }, { 'i.image': image })
    else await gql(q, { i: input })
    setForm({ type: 'EVENT', title: '', body: '', location: '', eventAt: '' })
    setImage(null)
    toast('Sent for review. It appears once approved.')
    onPosted()
  })
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value })
  return (
    <Modal open={open} onClose={onClose} title="Share with your campus" footer={<><Button variant="secondary" onClick={onClose}>Cancel</Button><Button loading={post.pending} onClick={() => post.run()}>Submit for review</Button></>}>
      <div className="space-y-4">
        <Field label="Type">
          <Select value={form.type} onChange={set('type')}>{postTypes.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}</Select>
        </Field>
        <Field label="Title"><Input maxLength={120} value={form.title} onChange={set('title')} /></Field>
        <Field label="Details"><Textarea maxLength={4000} rows={5} value={form.body} onChange={set('body')} /></Field>
        <div className="grid gap-4 sm:grid-cols-2">
          {form.type === 'EVENT' && <Field label="Date & time"><Input type="datetime-local" value={form.eventAt} onChange={set('eventAt')} /></Field>}
          <Field label="Location"><Input maxLength={200} value={form.location} onChange={set('location')} /></Field>
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-sm link">
          <ImagePlus className="h-4 w-4" /> {image ? image.name : 'Add an image (optional)'}
          <input type="file" accept="image/jpeg,image/png,image/webp" className="sr-only" onChange={(e) => setImage(e.target.files?.[0] ?? null)} />
        </label>
        <ErrorNote error={post.error} />
      </div>
    </Modal>
  )
}
