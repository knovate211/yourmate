// Keeps the interview room's shared state in step between the two browsers.
// Everything travels over LiveKit's data channel, so the room needs no server
// of its own beyond the video server:
//   - the code editor is a Yjs document (edits merge, never overwrite),
//   - cursors use Yjs awareness,
//   - whiteboard changes and chat are small JSON messages.
// LiveKit caps a reliable data packet at about 15 KB, so larger messages are
// split into chunks and put back together on arrival.

import * as Y from 'yjs'
import { Awareness, applyAwarenessUpdate, encodeAwarenessUpdate, removeAwarenessStates } from 'y-protocols/awareness'
import { RoomEvent, type RemoteParticipant, type Room } from 'livekit-client'

const CHUNK = 12_000
const TOPIC = 'ym'

// Message kinds, the first byte of every message.
const enum Kind {
  DocUpdate = 1, // a Yjs update
  DocState = 2, // a full Yjs state, sent to someone who just arrived
  Awareness = 3,
  Json = 4, // whiteboard, chat, problem changes
}

type JsonHandler = (msg: { type: string; [k: string]: unknown }, from: RemoteParticipant | undefined) => void

export class RoomSync {
  readonly doc = new Y.Doc()
  readonly awareness = new Awareness(this.doc)
  private seq = Math.floor(Math.random() * 1e9)
  private partial = new Map<string, { parts: Uint8Array[]; got: number }>()
  private handlers = new Set<JsonHandler>()
  private off: (() => void)[] = []

  constructor(private room: Room, name: string, color: string) {
    this.awareness.setLocalStateField('user', { name, color })

    const onDoc = (update: Uint8Array, origin: unknown) => {
      if (origin !== this) this.send(Kind.DocUpdate, update)
    }
    this.doc.on('update', onDoc)
    this.off.push(() => this.doc.off('update', onDoc))

    const onAwareness = ({ added, updated, removed }: { added: number[]; updated: number[]; removed: number[] }, origin: unknown) => {
      if (origin === this) return
      const changed = [...added, ...updated, ...removed]
      this.send(Kind.Awareness, encodeAwarenessUpdate(this.awareness, changed))
    }
    this.awareness.on('update', onAwareness)
    this.off.push(() => this.awareness.off('update', onAwareness))

    const onData = (payload: Uint8Array, from?: RemoteParticipant, _kind?: unknown, topic?: string) => {
      if (topic === TOPIC) this.receive(payload, from)
    }
    room.on(RoomEvent.DataReceived, onData)
    this.off.push(() => room.off(RoomEvent.DataReceived, onData))

    // Whoever arrives gets everyone's full state, and everyone gets theirs.
    const greet = () => {
      this.send(Kind.DocState, Y.encodeStateAsUpdate(this.doc))
      this.send(Kind.Awareness, encodeAwarenessUpdate(this.awareness, [this.doc.clientID]))
    }
    room.on(RoomEvent.ParticipantConnected, greet)
    this.off.push(() => room.off(RoomEvent.ParticipantConnected, greet))
    const onLeft = (p: RemoteParticipant) => {
      // Drop the cursor of whoever left.
      const gone = [...this.awareness.getStates().entries()].filter(([, s]) => (s as { identity?: string }).identity === p.identity).map(([id]) => id)
      if (gone.length) removeAwarenessStates(this.awareness, gone, this)
    }
    room.on(RoomEvent.ParticipantDisconnected, onLeft)
    this.off.push(() => room.off(RoomEvent.ParticipantDisconnected, onLeft))
    this.awareness.setLocalStateField('identity', room.localParticipant.identity)
  }

  /** Call once connected: shares this browser's state with anyone already there. */
  start() {
    if (this.room.remoteParticipants.size) {
      this.send(Kind.DocState, Y.encodeStateAsUpdate(this.doc))
      this.send(Kind.Awareness, encodeAwarenessUpdate(this.awareness, [this.doc.clientID]))
    }
  }

  /** Loads the editor state saved on the server (from an earlier visit). */
  loadSaved(state: Uint8Array) {
    Y.applyUpdate(this.doc, state, 'server')
  }

  onJson(h: JsonHandler) {
    this.handlers.add(h)
    return () => {
      this.handlers.delete(h)
    }
  }

  sendJson(msg: { type: string; [k: string]: unknown }) {
    this.send(Kind.Json, new TextEncoder().encode(JSON.stringify(msg)))
  }

  destroy() {
    removeAwarenessStates(this.awareness, [this.doc.clientID], 'local')
    this.off.forEach((f) => f())
    this.awareness.destroy()
    this.doc.destroy()
  }

  // Wire format: [kind][message id: 4][chunk index: 2][chunk count: 2][data]
  private send(kind: Kind, data: Uint8Array) {
    if (this.room.state !== 'connected' || this.room.remoteParticipants.size === 0) return
    const id = (this.seq = (this.seq + 1) >>> 0)
    const count = Math.max(1, Math.ceil(data.length / CHUNK))
    for (let i = 0; i < count; i++) {
      const part = data.subarray(i * CHUNK, (i + 1) * CHUNK)
      const msg = new Uint8Array(9 + part.length)
      const view = new DataView(msg.buffer)
      msg[0] = kind
      view.setUint32(1, id)
      view.setUint16(5, i)
      view.setUint16(7, count)
      msg.set(part, 9)
      void this.room.localParticipant.publishData(msg, { reliable: true, topic: TOPIC }).catch(() => {})
    }
  }

  private receive(msg: Uint8Array, from?: RemoteParticipant) {
    if (msg.length < 9) return
    const view = new DataView(msg.buffer, msg.byteOffset, msg.byteLength)
    const kind = msg[0] as Kind
    const id = view.getUint32(1)
    const index = view.getUint16(5)
    const count = view.getUint16(7)
    let data = msg.subarray(9)
    if (count > 1) {
      const key = `${from?.identity ?? ''}:${id}`
      const entry = this.partial.get(key) ?? { parts: new Array<Uint8Array>(count), got: 0 }
      if (!entry.parts[index]) {
        entry.parts[index] = data.slice()
        entry.got++
      }
      this.partial.set(key, entry)
      if (entry.got < count) return
      this.partial.delete(key)
      const total = entry.parts.reduce((n, p) => n + p.length, 0)
      data = new Uint8Array(total)
      let at = 0
      for (const p of entry.parts) {
        data.set(p, at)
        at += p.length
      }
    }
    switch (kind) {
      case Kind.DocUpdate:
      case Kind.DocState:
        Y.applyUpdate(this.doc, data, this)
        break
      case Kind.Awareness:
        applyAwarenessUpdate(this.awareness, data, this)
        break
      case Kind.Json:
        try {
          const parsed = JSON.parse(new TextDecoder().decode(data))
          this.handlers.forEach((h) => h(parsed, from))
        } catch {
          /* ignore malformed */
        }
        break
    }
  }
}

export function toBase64(bytes: Uint8Array): string {
  let s = ''
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(s)
}

export function fromBase64(b64: string): Uint8Array {
  const s = atob(b64)
  const out = new Uint8Array(s.length)
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i)
  return out
}
