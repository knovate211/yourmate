import { useEffect, useRef, useState } from 'react'
import { CaptureUpdateAction, Excalidraw, reconcileElements } from '@excalidraw/excalidraw'
import '@excalidraw/excalidraw/index.css'
import type { BoardStore } from '../lib/board'

// The interview room's whiteboard, for system design rounds. Loaded only when
// someone opens the Whiteboard tab: the drawing library is large.

// Fonts come from the same pinned version on a CDN instead of being bundled.
;(window as unknown as { EXCALIDRAW_ASSET_PATH: string }).EXCALIDRAW_ASSET_PATH = 'https://cdn.jsdelivr.net/npm/@excalidraw/excalidraw@0.18.1/dist/prod/'

type API = Parameters<NonNullable<React.ComponentProps<typeof Excalidraw>['excalidrawAPI']>>[0]

export default function Whiteboard({ store }: { store: BoardStore }) {
  const [api, setApi] = useState<API | null>(null)
  const applying = useRef(false)

  // Remote changes: merge them the way Excalidraw's own collaboration does.
  useEffect(() => {
    if (!api) return
    return store.onRemote((remote) => {
      applying.current = true
      const merged = reconcileElements(api.getSceneElementsIncludingDeleted(), remote as never, api.getAppState())
      api.updateScene({ elements: merged, captureUpdate: CaptureUpdateAction.NEVER })
      applying.current = false
    })
  }, [api, store])

  return (
    <div className="h-full w-full overflow-hidden rounded-xl">
      <Excalidraw
        excalidrawAPI={setApi}
        theme="dark"
        initialData={{ elements: store.elements() as never, scrollToContent: true }}
        UIOptions={{ tools: { image: false }, canvasActions: { loadScene: false, saveToActiveFile: false, export: false, saveAsImage: true } }}
        onChange={(elements) => {
          if (!applying.current) store.local(elements as unknown as readonly BoardElement[])
        }}
      />
    </div>
  )
}

type BoardElement = { id: string; version: number; versionNonce: number; isDeleted?: boolean }
