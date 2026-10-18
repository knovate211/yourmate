import { lazy, Suspense, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Video } from 'lucide-react'
import { formatTime } from '../lib/format'
import type { Booking } from '../lib/types'
import { cx } from './ui'

// Kept apart from the room page so the booking page doesn't download the
// video and editor libraries just to show a button.

const SavedCode = lazy(() => import('./CodeEditor').then((m) => ({ default: m.SavedCode })))

function useNow(ms: number) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(t)
  }, [ms])
  return now
}

/** For the booking page: the join button and, afterwards, what the room recorded. */
export function RoomCard({ booking: b }: { booking: Booking }) {
  const now = useNow(15_000)
  const r = b.room
  if (!r || (b.status !== 'ACCEPTED' && b.status !== 'COMPLETED')) return null
  const opens = new Date(r.opensAt).getTime()
  const closes = new Date(r.closesAt).getTime()
  const open = now >= opens && now <= closes
  const people = r.presence
  return (
    <section className="card space-y-3 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 font-semibold"><Video className="h-5 w-5 text-brand-500" /> Video room</h2>
          <p className="text-sm text-ink-600">
            {open ? 'The room is open now.' : now < opens ? `Opens at ${formatTime(r.opensAt)}, 10 minutes before the start.` : 'The room has closed.'} Video, a shared code editor and a whiteboard, right here in Your Mate.
          </p>
        </div>
        {now <= closes && <Link to={`/interview/${b.id}`} className={cx('inline-flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-semibold', open ? 'bg-brand-500 text-white hover:bg-brand-600' : 'border border-ink-200 bg-white text-ink-800')}>{open ? 'Join the room' : 'Check your camera'}</Link>}
      </div>
      {now > closes && r.code.trim() && (
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-ink-500">Final code</p>
          <Suspense fallback={<pre className="rounded-xl bg-ink-900 p-3 text-xs text-white">{r.code}</pre>}>
            <SavedCode code={r.code} language={r.language} />
          </Suspense>
        </div>
      )}
      {people.length > 0 && (
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-ink-500">Who was there</p>
          <ul className="mt-1 space-y-0.5 text-sm text-ink-700">
            {people.map((p, i) => <li key={i}>{p.user.displayName}: joined {formatTime(p.joinedAt)}{p.leftAt ? `, left ${formatTime(p.leftAt)}` : ', still in the room'}</li>)}
          </ul>
        </div>
      )}
    </section>
  )
}
