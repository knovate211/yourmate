import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  ConnectionQuality, LocalAudioTrack, LocalVideoTrack, Room, RoomEvent, Track, createLocalAudioTrack, createLocalVideoTrack,
  type Participant, type RemoteTrack,
} from 'livekit-client'
import * as Y from 'yjs'
import { ClipboardCheck, Code2, Mic, MicOff, MonitorUp, PenTool, PhoneOff, ScrollText, Send, Trash2, Video, VideoOff, Wifi, WifiOff } from 'lucide-react'
import { Container } from '../components/Layout'
import { Avatar, Badge, Button, Empty, ErrorNote, Input, Modal, PageLoader, Select, cx } from '../components/ui'
import { CodeEditor } from '../components/CodeEditor'
import { ReportButton } from '../components/domain'
import { useToast } from '../components/toast'
import { useAuth } from '../lib/auth'
import { errorMessage, gql } from '../lib/graphql'
import { useAction, useDebounced, useDocumentTitle, useQuery } from '../lib/hooks'
import { BOOKING_CARD, QUESTION, ROOM } from '../lib/fragments'
import { formatTime } from '../lib/format'
import { useCatalog } from '../lib/catalog'
import { RoomSync, fromBase64, toBase64 } from '../lib/roomSync'
import { BoardStore } from '../lib/board'
import type { Booking, MentorNote, NoteTone, Question } from '../lib/types'

const Whiteboard = lazy(() => import('../components/Whiteboard'))

const ROOM_BOOKING = `${BOOKING_CARD} providerUser { id } room { ${ROOM} } mentorNotes { id atSeconds body tone createdAt }`

const clock = (secs: number) => {
  const s = Math.max(0, Math.floor(secs))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

function useNow(ms = 1000) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(t)
  }, [ms])
  return now
}

function useWide() {
  const [wide, setWide] = useState(() => window.matchMedia('(min-width: 1024px)').matches)
  useEffect(() => {
    const m = window.matchMedia('(min-width: 1024px)')
    const on = () => setWide(m.matches)
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [])
  return wide
}

export function InterviewRoomPage() {
  const { id = '' } = useParams()
  const { user } = useAuth()
  const { data, loading, error, refetch } = useQuery<{ booking: Booking | null }>(`query($id: ID!) { booking(id: $id) { ${ROOM_BOOKING} } }`, { id }, { pollMs: 20_000 })
  const [live, setLive] = useState<{ room: Room; role: 'PROVIDER' | 'STUDENT' } | null>(null)
  const b = data?.booking
  useDocumentTitle(b ? 'Interview room' : '')

  if (loading && !data) return <PageLoader />
  if (error) return <Container><ErrorNote error={error} /></Container>
  if (!b || !user) return <Container><Empty title="Room not found">It may belong to someone else's session.</Empty></Container>
  if (!b.room) return <Container><Empty title="No video room for this session">In-person sessions happen at the agreed place.</Empty></Container>
  if (live) return <LiveRoom booking={b} room={live.room} role={live.role} onLeft={refetch} />
  return <Lobby booking={b} onJoined={setLive} />
}

// ── Lobby ───────────────────────────────────────────────────────────────────

function Lobby({ booking: b, onJoined }: { booking: Booking; onJoined: (l: { room: Room; role: 'PROVIDER' | 'STUDENT' }) => void }) {
  const { label } = useCatalog()
  const videoRef = useRef<HTMLVideoElement>(null)
  const [cams, setCams] = useState<MediaDeviceInfo[]>([])
  const [mics, setMics] = useState<MediaDeviceInfo[]>([])
  const [camId, setCamId] = useState('')
  const [micId, setMicId] = useState('')
  const [video, setVideo] = useState<LocalVideoTrack | null>(null)
  const [audio, setAudio] = useState<LocalAudioTrack | null>(null)
  const [camErr, setCamErr] = useState('')
  const [micErr, setMicErr] = useState('')
  const [level, setLevel] = useState(0)
  const [camOn, setCamOn] = useState(true)
  const now = useNow(5000)
  const room = b.room!
  const isOpen = now >= new Date(room.opensAt).getTime() && now <= new Date(room.closesAt).getTime()
  const other = b.myRole === 'PROVIDER' ? b.student : { id: b.providerUser?.id, displayName: b.provider.displayName }
  const otherHere = room.presence.some((p) => p.user.id === other.id && !p.leftAt)
  const what = b.interviewType ? `${label('interviewTypes', b.interviewType)} mock interview` : b.subject?.name ?? 'Session'

  // Camera preview.
  useEffect(() => {
    if (!camOn) return
    let track: LocalVideoTrack | undefined
    let cancelled = false
    createLocalVideoTrack(camId ? { deviceId: camId } : {})
      .then((t) => {
        if (cancelled) return t.stop()
        track = t
        setVideo(t)
        setCamErr('')
        if (videoRef.current) t.attach(videoRef.current)
      })
      .catch((e) => setCamErr(e?.name === 'NotAllowedError' ? 'Camera access is blocked. Allow it in the browser’s address bar, or join without video.' : 'No camera found.'))
    return () => {
      cancelled = true
      track?.stop()
      setVideo(null)
    }
  }, [camId, camOn])

  // Microphone preview with a level meter.
  useEffect(() => {
    let track: LocalAudioTrack | undefined
    let raf = 0
    let ctx: AudioContext | undefined
    let cancelled = false
    createLocalAudioTrack(micId ? { deviceId: micId } : {})
      .then((t) => {
        if (cancelled) return t.stop()
        track = t
        setAudio(t)
        setMicErr('')
        ctx = new AudioContext()
        const analyser = ctx.createAnalyser()
        analyser.fftSize = 256
        ctx.createMediaStreamSource(new MediaStream([t.mediaStreamTrack])).connect(analyser)
        const buf = new Uint8Array(analyser.frequencyBinCount)
        const tick = () => {
          analyser.getByteFrequencyData(buf)
          setLevel(Math.min(1, buf.reduce((a, v) => a + v, 0) / buf.length / 60))
          raf = requestAnimationFrame(tick)
        }
        tick()
      })
      .catch((e) => setMicErr(e?.name === 'NotAllowedError' ? 'Microphone access is blocked. Allow it in the browser’s address bar.' : 'No microphone found.'))
    return () => {
      cancelled = true
      cancelAnimationFrame(raf)
      void ctx?.close()
      track?.stop()
      setAudio(null)
    }
  }, [micId])

  // Device lists (labels appear once permission is granted).
  useEffect(() => {
    Room.getLocalDevices('videoinput').then(setCams).catch(() => {})
    Room.getLocalDevices('audioinput').then(setMics).catch(() => {})
  }, [video, audio])

  const join = useAction(async (withVideo: boolean) => {
    const d = await gql<{ joinRoom: { url: string; token: string; role: 'PROVIDER' | 'STUDENT' } }>(
      `mutation($b: ID!) { joinRoom(bookingId: $b) { url token role } }`, { b: b.id })
    // Hand the devices over: stop the previews first so the camera light doesn't flicker twice.
    video?.stop()
    audio?.stop()
    const room = new Room({
      adaptiveStream: true,
      dynacast: true,
      videoCaptureDefaults: camId ? { deviceId: camId } : undefined,
      audioCaptureDefaults: micId ? { deviceId: micId } : undefined,
    })
    await room.connect(d.joinRoom.url, d.joinRoom.token)
    await room.localParticipant.setMicrophoneEnabled(!micErr).catch(() => {})
    if (withVideo && !camErr) await room.localParticipant.setCameraEnabled(true).catch(() => {})
    onJoined({ room, role: d.joinRoom.role })
  })

  const start = new Date(b.startAt)
  return (
    <Container className="max-w-5xl">
      <Link to={`/bookings/${b.id}`} className="text-sm link">← Back to the booking</Link>
      <div className="mt-4 grid gap-6 lg:grid-cols-[1.3fr_1fr]">
        <div className="space-y-3">
          <div className="relative grid aspect-video place-items-center overflow-hidden rounded-2xl bg-[#1b1c25]">
            {camOn && !camErr ? <video ref={videoRef} className="h-full w-full -scale-x-100 object-cover" muted playsInline /> : <Avatar name="You" size={88} />}
            <div className="absolute inset-x-3 bottom-3 flex items-center gap-2">
              <button type="button" onClick={() => setCamOn((v) => !v)} className={cx('grid h-10 w-10 place-items-center rounded-full text-white', camOn ? 'bg-white/15 hover:bg-white/25' : 'bg-red-500')} aria-label={camOn ? 'Turn camera off' : 'Turn camera on'}>
                {camOn ? <Video className="h-4 w-4" /> : <VideoOff className="h-4 w-4" />}
              </button>
              <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-white/15" aria-label="Microphone level"><div className="h-full rounded-full bg-emerald-400 transition-[width]" style={{ width: `${Math.round(level * 100)}%` }} /></div>
              <span className="text-xs text-white/70">{micErr ? 'No microphone' : level > 0.08 ? 'We can hear you' : 'Say something'}</span>
            </div>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="text-sm"><span className="label">Camera</span>
              <Select value={camId} onChange={(e) => setCamId(e.target.value)}>
                <option value="">Default camera</option>
                {cams.map((d) => <option key={d.deviceId} value={d.deviceId}>{d.label || 'Camera'}</option>)}
              </Select></label>
            <label className="text-sm"><span className="label">Microphone</span>
              <Select value={micId} onChange={(e) => setMicId(e.target.value)}>
                <option value="">Default microphone</option>
                {mics.map((d) => <option key={d.deviceId} value={d.deviceId}>{d.label || 'Microphone'}</option>)}
              </Select></label>
          </div>
        </div>
        <div className="space-y-4">
          <div>
            <p className="text-sm text-ink-500">{what}</p>
            <h1 className="text-2xl font-bold">with {other.displayName}</h1>
            <p className="text-ink-600">{start.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}, {formatTime(b.startAt)} – {formatTime(b.endAt)} · {b.durationMinutes} min</p>
          </div>
          <ul className="space-y-2 text-sm">
            <Check ok={!camErr && camOn} label="Camera" note={camErr || (camOn ? 'Working' : 'Off')} />
            <Check ok={!micErr} label="Microphone" note={micErr || 'Working'} />
            <Check ok={otherHere} label={other.displayName} note={otherHere ? 'In the room' : 'Not here yet'} neutral />
          </ul>
          <ErrorNote error={join.error} />
          {isOpen ? (
            <div className="grid gap-2">
              <Button size="lg" loading={join.pending} onClick={() => join.run(camOn)}>Join interview</Button>
              {camOn && <Button variant="secondary" disabled={join.pending} onClick={() => join.run(false)}>Join with audio only</Button>}
            </div>
          ) : now < new Date(room.opensAt).getTime() ? (
            <p className="rounded-lg bg-ink-100 px-3 py-2 text-sm text-ink-700">The room opens at <b>{formatTime(room.opensAt)}</b>, 10 minutes before the start. Keep this page open.</p>
          ) : (
            <p className="rounded-lg bg-ink-100 px-3 py-2 text-sm text-ink-700">This room has closed. The code and notes are saved on the booking.</p>
          )}
          <p className="text-xs text-ink-500">Weak connection? Join with audio only: the code editor and whiteboard keep working. Coding rounds are easiest on a laptop.</p>
        </div>
      </div>
    </Container>
  )
}

function Check({ ok, label, note, neutral }: { ok: boolean; label: string; note: string; neutral?: boolean }) {
  return (
    <li className="flex items-center justify-between rounded-xl border border-ink-200 bg-white px-3 py-2.5">
      <span className="font-medium">{label}</span>
      <Badge tone={ok ? 'green' : neutral ? 'yellow' : 'red'}>{note}</Badge>
    </li>
  )
}

// ── Live room ───────────────────────────────────────────────────────────────

type ChatLine = { from: string; text: string; at: number; mine: boolean }

/** Re-renders on every room event that changes what's on screen. */
function useRoomTick(room: Room) {
  const [, setTick] = useState(0)
  useEffect(() => {
    const bump = () => setTick((t) => t + 1)
    const events = [
      RoomEvent.ParticipantConnected, RoomEvent.ParticipantDisconnected, RoomEvent.TrackSubscribed, RoomEvent.TrackUnsubscribed,
      RoomEvent.TrackMuted, RoomEvent.TrackUnmuted, RoomEvent.LocalTrackPublished, RoomEvent.LocalTrackUnpublished,
      RoomEvent.ActiveSpeakersChanged, RoomEvent.ConnectionQualityChanged, RoomEvent.Reconnecting, RoomEvent.Reconnected,
      RoomEvent.ConnectionStateChanged, RoomEvent.AudioPlaybackStatusChanged,
    ] as const
    events.forEach((e) => room.on(e, bump))
    return () => events.forEach((e) => room.off(e, bump))
  }, [room])
}

function LiveRoom({ booking: b, room, role, onLeft }: { booking: Booking; room: Room; role: 'PROVIDER' | 'STUDENT'; onLeft: () => void }) {
  const { label, codeLanguages } = useCatalog()
  const { user } = useAuth()
  const navigate = useNavigate()
  const toast = useToast()
  const wide = useWide()
  const now = useNow()
  useRoomTick(room)
  const isMentor = role === 'PROVIDER'
  const saved = b.room!

  // Shared state: one Yjs document (code, language, problem) and the board.
  const sync = useMemo(() => new RoomSync(room, user?.displayName ?? 'You', isMentor ? '#fb6f37' : '#7dcfff'), [room, user, isMentor])
  const [dirty, setDirty] = useState(0)
  const markDirty = useCallback(() => setDirty((d) => d + 1), [])
  const board = useMemo(() => new BoardStore(sync, room, saved.whiteboard, markDirty), [sync, room, saved.whiteboard, markDirty])
  const code = sync.doc.getText('code')
  const meta = sync.doc.getMap<string>('meta')
  const [language, setLanguage] = useState(saved.language || 'PYTHON')
  const [problem, setProblem] = useState(saved.problem)

  useEffect(() => {
    if (saved.codeState) sync.loadSaved(fromBase64(saved.codeState))
    else if (saved.code && code.length === 0) code.insert(0, saved.code)
    if (isMentor) {
      sync.doc.transact(() => {
        if (!meta.get('language')) meta.set('language', saved.language || 'PYTHON')
        if (meta.get('problem') === undefined) meta.set('problem', saved.problem)
      })
    }
    const read = () => {
      setLanguage(meta.get('language') || saved.language || 'PYTHON')
      setProblem(meta.get('problem') ?? saved.problem)
    }
    read()
    meta.observe(read)
    const onDoc = () => markDirty()
    sync.doc.on('update', onDoc)
    sync.start()
    return () => {
      meta.unobserve(read)
      sync.doc.off('update', onDoc)
    }
    // Runs once per room.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sync])

  // Save what the interview produced, a few seconds after each change.
  const save = useCallback(async () => {
    const input: Record<string, unknown> = {
      bookingId: b.id, code: code.toString(), codeState: toBase64(Y.encodeStateAsUpdate(sync.doc)), language, whiteboard: board.toJSON(),
    }
    if (isMentor) input.problem = problem
    await gql(`mutation($i: SaveRoomInput!) { saveRoom(input: $i) }`, { i: input }).catch(() => {})
  }, [b.id, code, sync, board, language, problem, isMentor])
  const debouncedDirty = useDebounced(dirty, 4000)
  useEffect(() => {
    if (debouncedDirty) void save()
  }, [debouncedDirty, save])

  // Chat.
  const [chat, setChat] = useState<ChatLine[]>([])
  const [unread, setUnread] = useState(0)
  useEffect(() => sync.onJson((m, from) => {
    if (m.type === 'chat' && typeof m.text === 'string') {
      setChat((c) => [...c.slice(-199), { from: from?.name || 'Them', text: String(m.text).slice(0, 1000), at: Date.now(), mine: false }])
      setUnread((n) => n + 1)
    }
  }), [sync])
  const sendChat = (text: string) => {
    sync.sendJson({ type: 'chat', text })
    setChat((c) => [...c, { from: 'You', text, at: Date.now(), mine: true }])
  }

  useEffect(() => {
    const onDisconnected = () => toast('You left the room.')
    room.on(RoomEvent.Disconnected, onDisconnected)
    return () => {
      room.off(RoomEvent.Disconnected, onDisconnected)
    }
  }, [room, toast])
  useEffect(() => () => {
    board.destroy()
    sync.destroy()
    void room.disconnect()
  }, [room, sync, board])

  const leave = async (toScorecard = false) => {
    await save()
    await room.disconnect()
    onLeft()
    navigate(`/bookings/${b.id}${toScorecard ? '#scorecard' : ''}`)
  }

  const me = room.localParticipant
  const other = [...room.remoteParticipants.values()][0]
  const otherName = isMentor ? b.student.displayName : b.provider.displayName
  const screenOwner = [me, ...room.remoteParticipants.values()].find((p) => p.isScreenShareEnabled)
  const elapsed = (now - new Date(b.startAt).getTime()) / 1000
  const reconnecting = room.state === 'reconnecting'

  type TabId = 'code' | 'board' | 'problem' | 'screen' | 'notes' | 'chat'
  const workTabs: { id: TabId; label: string; icon: JSX.Element }[] = [
    { id: 'code', label: 'Code', icon: <Code2 className="h-4 w-4" /> },
    { id: 'board', label: 'Whiteboard', icon: <PenTool className="h-4 w-4" /> },
    ...(!wide ? [{ id: 'problem' as const, label: 'Problem', icon: <ScrollText className="h-4 w-4" /> }] : []),
    ...(screenOwner ? [{ id: 'screen' as const, label: screenOwner === me ? 'Your screen' : 'Screen', icon: <MonitorUp className="h-4 w-4" /> }] : []),
    ...(!wide && isMentor ? [{ id: 'notes' as const, label: 'Notes', icon: <ClipboardCheck className="h-4 w-4" /> }] : []),
    ...(!wide ? [{ id: 'chat' as const, label: unread ? `Chat (${unread})` : 'Chat', icon: <Send className="h-4 w-4" /> }] : []),
  ]
  const [tab, setTab] = useState<TabId>('code')
  const [boardLoaded, setBoardLoaded] = useState(false)
  useEffect(() => {
    if (tab === 'board') setBoardLoaded(true)
    if (tab === 'chat') setUnread(0)
  }, [tab])
  useEffect(() => {
    if (screenOwner && screenOwner !== me) setTab('screen')
    else if (!screenOwner && tab === 'screen') setTab('code')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [!!screenOwner])

  const workspace = (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <div className="flex items-center gap-1 overflow-x-auto">
        {workTabs.map((t) => (
          <button key={t.id} type="button" onClick={() => setTab(t.id)}
            className={cx('flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm', tab === t.id ? 'bg-[#262838] text-white' : 'text-[#9a9db0] hover:text-white')}>
            {t.icon}{t.label}
          </button>
        ))}
        {tab === 'code' && (
          <select aria-label="Language" value={language} onChange={(e) => meta.set('language', e.target.value)}
            className="ml-auto rounded-lg border border-[#2d2f3b] bg-[#1b1c25] px-2 py-1 text-sm text-white">
            {(codeLanguages.length ? codeLanguages : [{ value: language, label: language }]).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
          </select>
        )}
      </div>
      <div className="relative min-h-[360px] flex-1">
        <div className={cx('absolute inset-0', tab !== 'code' && 'invisible')}>
          <CodeEditor text={code} awareness={sync.awareness} language={language} className="h-full overflow-hidden rounded-xl border border-[#2d2f3b]" />
        </div>
        {boardLoaded && (
          <div className={cx('absolute inset-0', tab !== 'board' && 'invisible')}>
            <Suspense fallback={<div className="grid h-full place-items-center text-sm text-[#9a9db0]">Loading the whiteboard…</div>}>
              <Whiteboard store={board} />
            </Suspense>
          </div>
        )}
        {tab === 'screen' && screenOwner && (
          <div className="absolute inset-0 overflow-hidden rounded-xl bg-black"><TrackVideo participant={screenOwner} source={Track.Source.ScreenShare} fit="contain" /></div>
        )}
        {tab === 'problem' && <div className="absolute inset-0 overflow-auto rounded-xl bg-[#1b1c25] p-4"><ProblemPanel isMentor={isMentor} problem={problem} onChange={(v) => meta.set('problem', v)} booking={b} /></div>}
        {tab === 'notes' && <div className="absolute inset-0 overflow-auto rounded-xl bg-[#1b1c25] p-3"><MentorPanel booking={b} elapsed={elapsed} onFinish={() => leave(true)} /></div>}
        {tab === 'chat' && <div className="absolute inset-0 flex flex-col rounded-xl bg-[#1b1c25] p-3"><ChatPanel lines={chat} onSend={sendChat} /></div>}
      </div>
      <p className="text-xs text-[#9a9db0]">Both of you can type and draw. Everything is saved to the booking as you go.</p>
    </div>
  )

  const videos = (
    <div className={cx('grid gap-2', wide ? 'grid-cols-1' : 'grid-cols-2')}>
      <Tile participant={other} name={otherName} waiting={!other} />
      <Tile participant={me} name="You" local />
    </div>
  )

  return (
    <div className="flex min-h-screen flex-col bg-[#111218] text-[#eceef5] lg:h-screen">
      <div className="flex flex-wrap items-center gap-3 border-b border-[#2d2f3b] px-4 py-2.5">
        <span className="text-sm text-[#9a9db0]">{b.interviewType ? `${label('interviewTypes', b.interviewType)} mock interview` : b.subject?.name ?? 'Session'} · with <b className="text-white">{otherName}</b></span>
        <span className="ml-auto flex items-center gap-2 text-sm">
          <Quality room={room} />
          <span className="rounded-md bg-[#1b1c25] px-2.5 py-1 font-semibold tabular-nums" aria-label="Interview clock">
            {elapsed < 0 ? `Starts in ${clock(-elapsed)}` : `${clock(elapsed)} / ${clock(b.durationMinutes * 60)}`}
          </span>
          <ReportButton targetType="BOOKING" targetId={b.id} label="Report" />
        </span>
      </div>
      {reconnecting && <div className="bg-amber-500/20 px-4 py-1.5 text-center text-sm text-amber-200">Connection dropped. Reconnecting…</div>}
      {!room.canPlaybackAudio && (
        <button type="button" onClick={() => room.startAudio()} className="bg-sky-500/20 px-4 py-1.5 text-center text-sm text-sky-100">Your browser paused the sound. Click to hear {otherName}.</button>
      )}
      <RemoteAudio room={room} />
      {wide ? (
        <div className="grid min-h-0 flex-1 grid-cols-[260px_1fr_300px]">
          <div className="space-y-3 border-r border-[#2d2f3b] p-3">{videos}<Controls room={room} onLeave={() => leave(false)} /></div>
          <div className="flex min-h-0 flex-col border-r border-[#2d2f3b] p-3">{workspace}</div>
          <SidePanel isMentor={isMentor} booking={b} problem={problem} setProblem={(v) => meta.set('problem', v)} elapsed={elapsed}
            onFinish={() => leave(true)} chat={chat} onSend={sendChat} unread={unread} clearUnread={() => setUnread(0)} />
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-3 p-3">
          {videos}
          <Controls room={room} onLeave={() => leave(false)} />
          {workspace}
        </div>
      )}
    </div>
  )
}

function Quality({ room }: { room: Room }) {
  const q = room.localParticipant.connectionQuality
  const bad = q === ConnectionQuality.Poor || q === ConnectionQuality.Lost
  return (
    <span className={cx('flex items-center gap-1 text-xs', bad ? 'text-amber-300' : 'text-emerald-300')}>
      {bad ? <WifiOff className="h-3.5 w-3.5" /> : <Wifi className="h-3.5 w-3.5" />}{bad ? 'Weak connection' : 'Connected'}
    </span>
  )
}

function TrackVideo({ participant, source, mirror, fit = 'cover' }: { participant: Participant; source: Track.Source; mirror?: boolean; fit?: 'cover' | 'contain' }) {
  const ref = useRef<HTMLVideoElement>(null)
  const track = participant.getTrackPublication(source)?.track
  useEffect(() => {
    const el = ref.current
    if (!track || !el) return
    track.attach(el)
    return () => {
      track.detach(el)
    }
  }, [track])
  return <video ref={ref} className={cx('h-full w-full', fit === 'cover' ? 'object-cover' : 'object-contain', mirror && '-scale-x-100')} muted playsInline autoPlay />
}

function Tile({ participant, name, local, waiting }: { participant?: Participant; name: string; local?: boolean; waiting?: boolean }) {
  const pub = participant?.getTrackPublication(Track.Source.Camera)
  const showVideo = !!pub?.track && !pub.isMuted
  const micOff = participant ? !participant.isMicrophoneEnabled : false
  return (
    <div className={cx('relative grid aspect-[4/3] place-items-center overflow-hidden rounded-xl bg-[#262838]', participant?.isSpeaking && 'ring-2 ring-emerald-400')}>
      {showVideo ? <TrackVideo participant={participant!} source={Track.Source.Camera} mirror={local} /> : <Avatar name={name} size={56} />}
      <span className="absolute bottom-1.5 left-2 flex items-center gap-1 rounded bg-black/40 px-1.5 py-0.5 text-xs">
        {micOff && <MicOff className="h-3 w-3 text-red-300" />}{waiting ? `Waiting for ${name}…` : name}
      </span>
    </div>
  )
}

/** Plays the other person's microphone. */
function RemoteAudio({ room }: { room: Room }) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const attach = (track: RemoteTrack) => {
      if (track.kind === Track.Kind.Audio && box.current) box.current.appendChild(track.attach())
    }
    const detach = (track: RemoteTrack) => track.detach().forEach((el) => el.remove())
    room.remoteParticipants.forEach((p) => p.audioTrackPublications.forEach((pub) => pub.track && attach(pub.track)))
    room.on(RoomEvent.TrackSubscribed, attach)
    room.on(RoomEvent.TrackUnsubscribed, detach)
    return () => {
      room.off(RoomEvent.TrackSubscribed, attach)
      room.off(RoomEvent.TrackUnsubscribed, detach)
    }
  }, [room])
  return <div ref={box} hidden />
}

function Controls({ room, onLeave }: { room: Room; onLeave: () => void }) {
  const me = room.localParticipant
  const [err, setErr] = useState('')
  const canShare = typeof navigator.mediaDevices?.getDisplayMedia === 'function'
  const run = (p: Promise<unknown>) => p.then(() => setErr('')).catch((e) => setErr(e?.name === 'NotAllowedError' ? 'Permission denied by the browser.' : errorMessage(e)))
  const btn = 'grid h-11 w-11 place-items-center rounded-full transition'
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-center gap-2">
        <button type="button" className={cx(btn, me.isMicrophoneEnabled ? 'bg-white/10 hover:bg-white/20' : 'bg-red-500')} onClick={() => run(me.setMicrophoneEnabled(!me.isMicrophoneEnabled))}
          aria-label={me.isMicrophoneEnabled ? 'Mute' : 'Unmute'}>{me.isMicrophoneEnabled ? <Mic className="h-5 w-5" /> : <MicOff className="h-5 w-5" />}</button>
        <button type="button" className={cx(btn, me.isCameraEnabled ? 'bg-white/10 hover:bg-white/20' : 'bg-red-500')} onClick={() => run(me.setCameraEnabled(!me.isCameraEnabled))}
          aria-label={me.isCameraEnabled ? 'Turn camera off' : 'Turn camera on'}>{me.isCameraEnabled ? <Video className="h-5 w-5" /> : <VideoOff className="h-5 w-5" />}</button>
        {canShare && (
          <button type="button" className={cx(btn, me.isScreenShareEnabled ? 'bg-sky-500' : 'bg-white/10 hover:bg-white/20')} onClick={() => run(me.setScreenShareEnabled(!me.isScreenShareEnabled))}
            aria-label={me.isScreenShareEnabled ? 'Stop sharing' : 'Share your screen'}><MonitorUp className="h-5 w-5" /></button>
        )}
        <button type="button" className={cx(btn, 'bg-red-500 hover:bg-red-600')} onClick={onLeave} aria-label="Leave the room"><PhoneOff className="h-5 w-5" /></button>
      </div>
      {err && <p className="text-center text-xs text-red-300">{err}</p>}
    </div>
  )
}

// ── Side panel ──────────────────────────────────────────────────────────────

function SidePanel({ isMentor, booking, problem, setProblem, elapsed, onFinish, chat, onSend, unread, clearUnread }: {
  isMentor: boolean; booking: Booking; problem: string; setProblem: (v: string) => void; elapsed: number; onFinish: () => void
  chat: ChatLine[]; onSend: (t: string) => void; unread: number; clearUnread: () => void
}) {
  const [tab, setTab] = useState<'main' | 'problem' | 'chat'>(isMentor ? 'main' : 'problem')
  const tabs = isMentor ? (['main', 'problem', 'chat'] as const) : (['problem', 'chat'] as const)
  const names = { main: 'Notes', problem: 'Problem', chat: unread ? `Chat (${unread})` : 'Chat' }
  return (
    <div className="flex min-h-0 flex-col gap-2 p-3">
      <div className="flex gap-1">
        {tabs.map((t) => (
          <button key={t} type="button" onClick={() => { setTab(t); if (t === 'chat') clearUnread() }}
            className={cx('rounded-lg px-3 py-1.5 text-sm', tab === t ? 'bg-[#262838] text-white' : 'text-[#9a9db0] hover:text-white')}>{names[t]}</button>
        ))}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {tab === 'main' && <MentorPanel booking={booking} elapsed={elapsed} onFinish={onFinish} />}
        {tab === 'problem' && <ProblemPanel isMentor={isMentor} problem={problem} onChange={setProblem} booking={booking} />}
        {tab === 'chat' && <div className="flex h-full flex-col"><ChatPanel lines={chat} onSend={onSend} /></div>}
      </div>
    </div>
  )
}

function ProblemPanel({ isMentor, problem, onChange, booking }: { isMentor: boolean; problem: string; onChange: (v: string) => void; booking: Booking }) {
  const [picking, setPicking] = useState(false)
  if (!isMentor) {
    return problem ? <p className="whitespace-pre-wrap text-sm leading-relaxed">{problem}</p> : <p className="text-sm text-[#9a9db0]">Your mentor will share the problem here.</p>
  }
  return (
    <div className="space-y-2">
      <textarea value={problem} onChange={(e) => onChange(e.target.value.slice(0, 10_000))} rows={10} placeholder="Paste or write the problem. The student sees it as you type."
        className="w-full rounded-lg border border-[#2d2f3b] bg-[#15161e] p-2 text-sm text-white placeholder:text-[#6b7089]" aria-label="Problem statement" />
      <button type="button" className="text-sm font-medium text-[#fb6f37] hover:underline" onClick={() => setPicking(true)}>Pick from the question bank</button>
      {picking && <QuestionPicker booking={booking} onClose={() => setPicking(false)} onPick={(q) => {
        onChange(`${q.title}\n\n${q.body}`)
        void gql(`mutation($i: SaveRoomInput!) { saveRoom(input: $i) }`, { i: { bookingId: booking.id, questionId: q.id, problem: `${q.title}\n\n${q.body}` } })
        setPicking(false)
      }} />}
    </div>
  )
}

function QuestionPicker({ booking, onClose, onPick }: { booking: Booking; onClose: () => void; onPick: (q: Question) => void }) {
  const { label } = useCatalog()
  const [q, setQ] = useState('')
  const query = useDebounced(q, 300)
  const { data, loading, error } = useQuery<{ questions: { items: Question[] } }>(
    `query($q: String, $t: InterviewType) { questions(query: $q, interviewType: $t, pageSize: 30) { items { ${QUESTION} } } }`,
    { q: query || null, t: booking.interviewType },
  )
  return (
    <Modal open onClose={onClose} title="Question bank">
      <div className="space-y-3">
        <Input placeholder="Search questions" value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
        <ErrorNote error={error} />
        {loading && !data ? <p className="text-sm text-ink-500">Loading…</p> : data?.questions.items.length ? (
          <ul className="max-h-80 divide-y divide-ink-100 overflow-y-auto">
            {data.questions.items.map((x) => (
              <li key={x.id}><button type="button" onClick={() => onPick(x)} className="w-full px-1 py-2.5 text-left hover:bg-ink-50">
                <p className="font-medium">{x.title}</p>
                <p className="text-xs text-ink-500">{label('interviewTypes', x.interviewType)} · {x.difficulty.toLowerCase()}{x.companies.length ? ` · ${x.companies.join(', ')}` : ''}</p>
              </button></li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-ink-500">No questions yet. <Link to="/prep/questions" className="link">Add some to the bank</Link>.</p>
        )}
      </div>
    </Modal>
  )
}

function ChatPanel({ lines, onSend }: { lines: ChatLine[]; onSend: (t: string) => void }) {
  const [text, setText] = useState('')
  const end = useRef<HTMLDivElement>(null)
  useEffect(() => {
    end.current?.scrollIntoView({ block: 'nearest' })
  }, [lines.length])
  return (
    <>
      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto">
        {lines.length === 0 && <p className="text-sm text-[#9a9db0]">Messages here aren't saved. Use them for links and quick notes.</p>}
        {lines.map((l, i) => (
          <div key={i} className={cx('max-w-[90%] rounded-lg px-2.5 py-1.5 text-sm', l.mine ? 'ml-auto bg-[#e8490f] text-white' : 'bg-[#262838]')}>
            {!l.mine && <p className="text-[11px] text-[#9a9db0]">{l.from}</p>}<p className="whitespace-pre-wrap break-words">{l.text}</p>
          </div>
        ))}
        <div ref={end} />
      </div>
      <form className="mt-2 flex gap-2" onSubmit={(e) => { e.preventDefault(); if (text.trim()) { onSend(text.trim()); setText('') } }}>
        <input value={text} onChange={(e) => setText(e.target.value)} maxLength={1000} placeholder="Message"
          className="min-w-0 flex-1 rounded-lg border border-[#2d2f3b] bg-[#15161e] px-2.5 py-1.5 text-sm text-white placeholder:text-[#6b7089]" aria-label="Chat message" />
        <button type="submit" className="grid h-9 w-9 place-items-center rounded-lg bg-[#e8490f]" aria-label="Send"><Send className="h-4 w-4" /></button>
      </form>
    </>
  )
}

const QUICK_TAGS: { body: string; tone: NoteTone }[] = [
  { body: 'Good clarifying question', tone: 'PLUS' },
  { body: 'Explained the approach clearly', tone: 'PLUS' },
  { body: 'Missed an edge case', tone: 'MINUS' },
  { body: 'Started coding before planning', tone: 'MINUS' },
  { body: 'Needed a hint', tone: 'NOTE' },
]

function MentorPanel({ booking: b, elapsed, onFinish }: { booking: Booking; elapsed: number; onFinish: () => void }) {
  const { feedbackCriteria } = useCatalog()
  const [notes, setNotes] = useState<MentorNote[]>(b.mentorNotes ?? [])
  const [text, setText] = useState('')
  const [tone, setTone] = useState<NoteTone>('NOTE')
  const [scores, setScores] = useState<Record<string, number>>(Object.fromEntries((b.room?.draftScores ?? []).map((s) => [s.criterion, s.score])))
  const add = useAction(async (body: string, t: NoteTone) => {
    const d = await gql<{ addMentorNote: MentorNote }>(`mutation($b: ID!, $s: Int!, $body: String!, $t: NoteTone) { addMentorNote(bookingId: $b, atSeconds: $s, body: $body, tone: $t) { id atSeconds body tone createdAt } }`,
      { b: b.id, s: Math.max(0, Math.floor(elapsed)), body, t })
    setNotes((n) => [...n, d.addMentorNote].sort((x, y) => x.atSeconds - y.atSeconds))
  })
  const remove = async (id: string) => {
    setNotes((n) => n.filter((x) => x.id !== id))
    await gql(`mutation($id: ID!) { deleteMentorNote(id: $id) }`, { id }).catch(() => {})
  }
  const setScore = (criterion: string, score: number) => {
    const next = { ...scores, [criterion]: score }
    setScores(next)
    void gql(`mutation($i: SaveRoomInput!) { saveRoom(input: $i) }`, {
      i: { bookingId: b.id, draftScores: Object.entries(next).map(([c, s]) => ({ criterion: c, score: s })) },
    }).catch(() => {})
  }
  const toneDot: Record<NoteTone, string> = { PLUS: 'bg-emerald-400', MINUS: 'bg-amber-400', NOTE: 'bg-[#9a9db0]' }
  return (
    <div className="space-y-3">
      <section className="space-y-2 rounded-xl bg-[#1b1c25] p-3">
        <p className="text-xs font-semibold uppercase tracking-wide text-[#9a9db0]">Your notes · only you see these</p>
        <ul className="space-y-1.5">
          {notes.map((n) => (
            <li key={n.id} className="group flex items-start gap-2 text-sm">
              <span className={cx('mt-1.5 h-2 w-2 shrink-0 rounded-full', toneDot[n.tone])} />
              <b className="shrink-0 font-semibold tabular-nums text-[#fb6f37]">{clock(n.atSeconds)}</b>
              <span className="min-w-0 flex-1 break-words">{n.body}</span>
              <button type="button" onClick={() => remove(n.id)} className="opacity-0 group-hover:opacity-100 focus:opacity-100" aria-label="Delete note"><Trash2 className="h-3.5 w-3.5 text-[#9a9db0]" /></button>
            </li>
          ))}
          {notes.length === 0 && <li className="text-sm text-[#9a9db0]">Notes are stamped with the interview clock.</li>}
        </ul>
        <div className="flex flex-wrap gap-1">
          {QUICK_TAGS.map((t) => (
            <button key={t.body} type="button" onClick={() => add.run(t.body, t.tone)} className="rounded-full border border-[#2d2f3b] px-2 py-0.5 text-xs text-[#9a9db0] hover:text-white">+ {t.body}</button>
          ))}
        </div>
        <form className="flex gap-1.5" onSubmit={(e) => { e.preventDefault(); if (text.trim()) { add.run(text.trim(), tone); setText('') } }}>
          <select value={tone} onChange={(e) => setTone(e.target.value as NoteTone)} aria-label="Kind of note" className="rounded-lg border border-[#2d2f3b] bg-[#15161e] px-1 text-xs text-white">
            <option value="NOTE">Note</option><option value="PLUS">Strength</option><option value="MINUS">To work on</option>
          </select>
          <input value={text} onChange={(e) => setText(e.target.value)} maxLength={500} placeholder={`Note at ${clock(elapsed)}`}
            className="min-w-0 flex-1 rounded-lg border border-[#2d2f3b] bg-[#15161e] px-2 py-1 text-sm text-white placeholder:text-[#6b7089]" aria-label="New note" />
        </form>
        {add.error ? <p className="text-xs text-red-300">{errorMessage(add.error)}</p> : null}
      </section>
      <section className="space-y-2 rounded-xl bg-[#1b1c25] p-3">
        <p className="text-xs font-semibold uppercase tracking-wide text-[#9a9db0]">Scores so far</p>
        {feedbackCriteria.map((c) => (
          <div key={c.value} className="flex items-center justify-between gap-2 text-sm">
            <span>{c.label}</span>
            <span className="flex gap-1" role="radiogroup" aria-label={c.label}>
              {[1, 2, 3, 4, 5].map((i) => (
                <button key={i} type="button" role="radio" aria-checked={scores[c.value] === i} onClick={() => setScore(c.value, i)}
                  className={cx('h-5 w-5 rounded text-[10px] font-semibold', (scores[c.value] ?? 0) >= i ? 'bg-[#fb6f37] text-white' : 'bg-[#2d2f3b] text-[#9a9db0]')}>{i}</button>
              ))}
            </span>
          </div>
        ))}
        <Button className="mt-1 w-full" onClick={onFinish}><ClipboardCheck className="h-4 w-4" /> Finish & write scorecard</Button>
      </section>
    </div>
  )
}
