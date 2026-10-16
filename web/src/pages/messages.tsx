import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Lock, MessageCircle, Send, ShieldOff } from 'lucide-react'
import { Container } from '../components/Layout'
import { ReportButton } from '../components/domain'
import { Avatar, Badge, Button, Empty, ErrorNote, Input, PageLoader, cx } from '../components/ui'
import { useToast } from '../components/toast'
import { gql } from '../lib/graphql'
import { useAction, useDocumentTitle, useQuery } from '../lib/hooks'
import { CONVERSATION } from '../lib/fragments'
import { useMessageEvents } from '../lib/realtime'
import { formatDateTime, timeAgo } from '../lib/format'
import type { Conversation, DirectMessage, Page } from '../lib/types'

type Folder = 'INBOX' | 'REQUESTS'

/** A "Message" button for someone's profile or a member list. Hidden when
 *  you can't message them (no shared community, or a block). */
export function MessageButton({ userId, size = 'sm' }: { userId: string; size?: 'sm' | 'md' }) {
  const navigate = useNavigate()
  const { data } = useQuery<{ canMessage: { allowed: boolean; conversationId: string | null } }>(
    `query($u: ID!) { canMessage(userId: $u) { allowed conversationId } }`,
    { u: userId },
  )
  const open = useAction(async () => {
    if (data?.canMessage.conversationId) return navigate(`/messages/${data.canMessage.conversationId}`)
    const d = await gql<{ startConversation: { id: string } }>(`mutation($u: ID!) { startConversation(userId: $u) { id } }`, { u: userId })
    navigate(`/messages/${d.startConversation.id}`)
  })
  if (!data?.canMessage.allowed) return null
  return (
    <Button size={size} variant="secondary" loading={open.pending} onClick={() => open.run()}>
      <MessageCircle className="h-4 w-4" /> Message
    </Button>
  )
}

export function Messages() {
  useDocumentTitle('Messages')
  const { id } = useParams()
  const [folder, setFolder] = useState<Folder>('INBOX')
  const { data: summary, refetch: refetchSummary } = useQuery<{ inboxSummary: { unread: number; requests: number } }>(`{ inboxSummary { unread requests } }`, undefined, { pollMs: 60_000 })
  const { data, loading, refetch: refetchList } = useQuery<{ conversations: Page<Conversation> }>(
    `query($f: ConversationFolder!) { conversations(folder: $f, pageSize: 50) { total items { ${CONVERSATION} } } }`,
    { f: folder },
    { pollMs: 60_000 },
  )
  const refetch = () => {
    refetchList()
    refetchSummary()
  }
  // New messages reorder the list and change unread counts.
  useMessageEvents(refetch)
  const items = data?.conversations.items ?? []
  const requests = summary?.inboxSummary.requests ?? 0

  return (
    <Container className="max-w-6xl">
      <h1 className="mb-4 text-2xl font-bold">Messages</h1>
      <div className="card grid min-h-[70vh] overflow-hidden md:grid-cols-[320px_1fr]">
        <aside className={cx('flex flex-col border-ink-100 md:border-r', id && 'hidden md:flex')}>
          <div className="flex gap-1 border-b border-ink-100 p-2" role="tablist">
            {(['INBOX', 'REQUESTS'] as Folder[]).map((f) => (
              <button
                key={f}
                role="tab"
                aria-selected={folder === f}
                onClick={() => setFolder(f)}
                className={cx('flex-1 rounded-lg px-3 py-2 text-sm font-medium', folder === f ? 'bg-ink-900 text-white' : 'text-ink-600 hover:bg-ink-50')}
              >
                {f === 'INBOX' ? 'Inbox' : 'Requests'}
                {f === 'REQUESTS' && requests > 0 && <span className="ml-1.5 rounded-full bg-brand-500 px-1.5 text-[11px] font-bold text-white">{requests}</span>}
              </button>
            ))}
          </div>
          <div className="flex-1 overflow-y-auto">
            {loading && !data ? (
              <PageLoader />
            ) : items.length ? (
              <ul className="divide-y divide-ink-100">
                {items.map((c) => (
                  <li key={c.id}>
                    <Link to={`/messages/${c.id}`} className={cx('flex gap-3 px-4 py-3 hover:bg-ink-50', c.id === id && 'bg-brand-50/70')}>
                      <Avatar name={c.other.displayName} url={c.other.avatarUrl} size={40} />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-baseline justify-between gap-2">
                          <p className={cx('truncate text-sm', c.unreadCount ? 'font-bold text-ink-900' : 'font-semibold text-ink-800')}>{c.other.displayName}</p>
                          <span className="shrink-0 text-[11px] text-ink-400">{timeAgo(c.updatedAt)}</span>
                        </div>
                        <p className={cx('truncate text-xs', c.unreadCount ? 'text-ink-800' : 'text-ink-500')}>
                          {c.lastMessage ? `${c.lastMessage.mine ? 'You: ' : ''}${c.lastMessage.body}` : 'No messages yet'}
                        </p>
                      </div>
                      {c.unreadCount > 0 && <span className="mt-1 h-5 min-w-5 self-start rounded-full bg-brand-500 px-1.5 text-center text-[11px] font-bold leading-5 text-white">{c.unreadCount}</span>}
                    </Link>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="px-4 py-10 text-center text-sm text-ink-500">
                {folder === 'INBOX' ? 'No chats yet. Open someone’s profile from a community you share and tap Message.' : 'No message requests.'}
              </p>
            )}
          </div>
        </aside>
        <section className={cx('flex min-w-0 flex-col', !id && 'hidden md:flex')}>
          {id ? (
            <Thread key={id} id={id} onChange={refetch} />
          ) : (
            <div className="m-auto p-8 text-center text-sm text-ink-500">
              <MessageCircle className="mx-auto mb-2 h-10 w-10 text-ink-300" />
              Choose a chat to read it. You can message anyone who shares a community with you.
            </div>
          )}
        </section>
      </div>
    </Container>
  )
}

function Thread({ id, onChange }: { id: string; onChange: () => void }) {
  const toast = useToast()
  const navigate = useNavigate()
  const [body, setBody] = useState('')
  const bottom = useRef<HTMLDivElement>(null)
  const { data, loading, error, refetch } = useQuery<{ conversation: Conversation | null }>(
    `query($id: ID!) { conversation(id: $id) { ${CONVERSATION} } }`,
    { id },
    { pollMs: 60_000 },
  )
  const { data: msgs, refetch: refetchMessages } = useQuery<{ directMessages: DirectMessage[] }>(
    `query($id: ID!) { directMessages(conversationId: $id) { id body createdAt mine } }`,
    { id },
    { pollMs: 30_000 },
  )
  // Messages arrive live; the slow poll only covers a dropped socket.
  useMessageEvents((e) => {
    if (e.conversationId !== id) return
    refetchMessages()
    // The first reply to a request turns it into a chat.
    if (data?.conversation?.status === 'REQUEST') refetch()
  })
  const messages = msgs?.directMessages ?? []
  useEffect(() => {
    bottom.current?.scrollIntoView({ block: 'nearest' })
  }, [messages.length])
  // Opening a chat marks it read, so refresh the list's unread counts.
  useEffect(() => {
    if (msgs) onChange()
  }, [msgs?.directMessages.length]) // eslint-disable-line react-hooks/exhaustive-deps

  const send = useAction(async () => {
    await gql(`mutation($c: ID!, $b: String!) { sendDirectMessage(conversationId: $c, body: $b) { id } }`, { c: id, b: body })
    setBody('')
    refetchMessages()
    refetch()
    onChange()
  })
  const respond = useAction(async (accept: boolean) => {
    await gql(`mutation($id: ID!, $a: Boolean!) { respondToConversation(id: $id, accept: $a) { id } }`, { id, a: accept })
    toast(accept ? 'Request accepted.' : 'Request ignored. They won’t be told.')
    onChange()
    if (accept) refetch()
    else navigate('/messages')
  })
  const block = useAction(async (userId: string) => {
    await gql(`mutation($u: ID!) { blockUser(userId: $u) }`, { u: userId })
    toast('Blocked. You can undo this in Settings.')
    onChange()
    navigate('/messages')
  })

  if (loading && !data) return <PageLoader />
  if (error) return <ErrorNote error={error} className="m-4" />
  const c = data?.conversation
  if (!c) return <Empty title="This chat isn't available" />
  const pendingMine = c.status === 'REQUEST' && !c.isRequestForMe

  return (
    <>
      <header className="flex items-center gap-3 border-b border-ink-100 px-4 py-3">
        <Link to="/messages" className="rounded-lg p-1 text-ink-500 hover:bg-ink-100 md:hidden" aria-label="Back to chats">
          <ArrowLeft className="h-5 w-5" />
        </Link>
        <Avatar name={c.other.displayName} url={c.other.avatarUrl} size={36} />
        <div className="min-w-0 flex-1">
          <Link to={`/users/${c.other.id}`} className="block truncate font-semibold hover:underline">{c.other.displayName}</Link>
          {c.other.campusVerified && <p className="text-xs text-ink-500">Verified student</p>}
        </div>
        {c.status === 'REQUEST' && <Badge tone="yellow">Request</Badge>}
        <ReportButton targetType="USER" targetId={c.other.id} />
      </header>

      {c.isRequestForMe && (
        <div className="flex flex-col gap-3 border-b border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 sm:flex-row sm:items-center sm:justify-between">
          <p><strong>{c.other.displayName}</strong> wants to message you. They won't know you've read this until you reply or accept.</p>
          <div className="flex shrink-0 gap-2">
            <Button size="sm" loading={respond.pending} onClick={() => respond.run(true)}>Accept</Button>
            <Button size="sm" variant="secondary" onClick={() => respond.run(false)}>Ignore</Button>
            <Button size="sm" variant="ghost" className="text-red-600" onClick={() => block.run(c.other.id)}>Block</Button>
          </div>
        </div>
      )}

      <div className="flex-1 space-y-2 overflow-y-auto px-4 py-4" aria-live="polite">
        {messages.length === 0 && <p className="py-10 text-center text-sm text-ink-500">Say hello to {c.other.displayName.split(' ')[0]}.</p>}
        {messages.map((m, i) => {
          const showTime = i === 0 || new Date(m.createdAt).getTime() - new Date(messages[i - 1].createdAt).getTime() > 15 * 60_000
          return (
            <div key={m.id}>
              {showTime && <p className="my-2 text-center text-[11px] text-ink-400">{formatDateTime(m.createdAt)}</p>}
              <div className={cx('flex', m.mine ? 'justify-end' : 'justify-start')}>
                <p className={cx('max-w-[75%] whitespace-pre-line break-words rounded-2xl px-3.5 py-2 text-sm', m.mine ? 'rounded-br-sm bg-brand-500 text-white' : 'rounded-bl-sm bg-ink-100 text-ink-900')}>
                  {m.body}
                </p>
              </div>
            </div>
          )
        })}
        <div ref={bottom} />
      </div>

      {c.canSend ? (
        <form className="flex gap-2 border-t border-ink-100 p-3" onSubmit={(e) => { e.preventDefault(); if (body.trim()) send.run() }}>
          <Input value={body} onChange={(e) => setBody(e.target.value)} maxLength={2000} placeholder={pendingMine ? 'They’ll see this when they accept' : 'Write a message'} aria-label="Message" autoFocus />
          <Button type="submit" loading={send.pending} disabled={!body.trim()} aria-label="Send"><Send className="h-4 w-4" /></Button>
        </form>
      ) : (
        <p className="flex items-center justify-center gap-2 border-t border-ink-100 px-4 py-3 text-center text-xs text-ink-500">
          {!c.sharesCommunity ? (
            <><Lock className="h-3.5 w-3.5" /> You no longer share a community, so this chat is read-only.</>
          ) : pendingMine ? (
            <>Waiting for {c.other.displayName.split(' ')[0]} to accept your request.</>
          ) : (
            <><ShieldOff className="h-3.5 w-3.5" /> You can't send messages in this chat.</>
          )}
        </p>
      )}
      {send.error ? <ErrorNote error={send.error} className="mx-3 mb-3" /> : null}
      {pendingMine && c.canSend && <p className="px-4 pb-3 text-center text-[11px] text-ink-400">Until they accept, you can send a few messages.</p>}
    </>
  )
}
