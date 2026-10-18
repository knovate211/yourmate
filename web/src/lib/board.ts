// The whiteboard's shared state, kept outside the drawing component so
// changes from the other person arrive even while the Whiteboard tab is
// closed (the drawing library loads only when the tab is first opened).
//
// Elements merge the way Excalidraw's own collaboration does: per element,
// the higher version wins, and on a tie the lower versionNonce wins, so both
// browsers settle on the same picture.

import { RoomEvent, type Room } from 'livekit-client'
import type { RoomSync } from './roomSync'

export interface BoardElement {
  id: string
  version: number
  versionNonce: number
  isDeleted?: boolean
  [k: string]: unknown
}

const newer = (a: BoardElement, b: BoardElement | undefined) =>
  !b || a.version > b.version || (a.version === b.version && a.versionNonce < b.versionNonce)

export class BoardStore {
  private byId = new Map<string, BoardElement>()
  private listeners = new Set<(els: BoardElement[]) => void>()
  private outbox = new Map<string, BoardElement>()
  private flushTimer: ReturnType<typeof setTimeout> | undefined
  private off: (() => void)[] = []

  constructor(private sync: RoomSync, room: Room, saved: string, private onDirty: () => void) {
    try {
      for (const el of JSON.parse(saved || '[]') as BoardElement[]) if (el?.id) this.byId.set(el.id, el)
    } catch {
      /* start empty */
    }
    this.off.push(
      sync.onJson((msg) => {
        if (msg.type !== 'wb' || !Array.isArray(msg.elements)) return
        const changed = (msg.elements as BoardElement[]).filter((el) => el?.id && newer(el, this.byId.get(el.id)))
        changed.forEach((el) => this.byId.set(el.id, el))
        if (changed.length) {
          this.listeners.forEach((l) => l(changed))
          this.onDirty()
        }
      }),
    )
    // Whoever arrives gets the whole board.
    const greet = () => this.sync.sendJson({ type: 'wb', elements: [...this.byId.values()] })
    room.on(RoomEvent.ParticipantConnected, greet)
    this.off.push(() => room.off(RoomEvent.ParticipantConnected, greet))
  }

  elements(): BoardElement[] {
    return [...this.byId.values()].filter((el) => !el.isDeleted)
  }

  /** The current board as saved on the server: deleted elements left out. */
  toJSON(): string {
    return JSON.stringify(this.elements())
  }

  /** Changes drawn in this browser. */
  local(elements: readonly BoardElement[]) {
    let any = false
    for (const el of elements) {
      if (newer(el, this.byId.get(el.id))) {
        this.byId.set(el.id, el)
        this.outbox.set(el.id, el)
        any = true
      }
    }
    if (!any) return
    this.onDirty()
    // Batch rapid strokes into one message every 80 ms.
    this.flushTimer ??= setTimeout(() => {
      this.flushTimer = undefined
      const batch = [...this.outbox.values()]
      this.outbox.clear()
      if (batch.length) this.sync.sendJson({ type: 'wb', elements: batch })
    }, 80)
  }

  onRemote(l: (els: BoardElement[]) => void) {
    this.listeners.add(l)
    return () => {
      this.listeners.delete(l)
    }
  }

  destroy() {
    clearTimeout(this.flushTimer)
    this.off.forEach((f) => f())
  }
}
