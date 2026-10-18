import { useEffect, useRef, useState } from 'react'
import * as pdfjs from 'pdfjs-dist'
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import type { ResumePin } from '../lib/types'
import { cx } from './ui'

// Shows a resume PDF with numbered pins on it. The mentor clicks the page to
// place a pin; pins are stored as fractions of the page, so they land in the
// same spot at any screen width. Loaded only on the resume review page.

pdfjs.GlobalWorkerOptions.workerSrc = workerUrl

const MAX_PAGES = 8

export default function PdfPins({ url, pins, editable, selected, onSelect, onAdd }: {
  url: string
  pins: Pick<ResumePin, 'page' | 'x' | 'y'>[]
  editable?: boolean
  selected?: number | null
  onSelect?: (index: number) => void
  onAdd?: (page: number, x: number, y: number) => void
}) {
  const box = useRef<HTMLDivElement>(null)
  const [pages, setPages] = useState<{ url: string; w: number; h: number }[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const doc = await pdfjs.getDocument({ url }).promise
        const out: { url: string; w: number; h: number }[] = []
        for (let n = 1; n <= Math.min(doc.numPages, MAX_PAGES); n++) {
          const page = await doc.getPage(n)
          const viewport = page.getViewport({ scale: 1.6 })
          const canvas = document.createElement('canvas')
          canvas.width = viewport.width
          canvas.height = viewport.height
          await page.render({ canvas, canvasContext: canvas.getContext('2d')!, viewport }).promise
          out.push({ url: canvas.toDataURL('image/png'), w: viewport.width, h: viewport.height })
        }
        if (!cancelled) setPages(out)
      } catch {
        if (!cancelled) setError('This PDF could not be shown here. Open it with the link above instead.')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [url])

  if (error) return <p className="rounded-lg bg-ink-100 p-3 text-sm text-ink-600">{error}</p>
  if (!pages.length) return <div className="grid h-64 place-items-center rounded-xl bg-ink-100 text-sm text-ink-500">Loading the resume…</div>
  return (
    <div ref={box} className="space-y-3 rounded-xl bg-ink-100 p-3">
      {pages.map((p, i) => (
        <div key={i} className={cx('relative overflow-hidden rounded bg-white shadow-sm', editable && 'cursor-crosshair')}
          onClick={(e) => {
            if (!editable || !onAdd) return
            const r = e.currentTarget.getBoundingClientRect()
            onAdd(i + 1, (e.clientX - r.left) / r.width, (e.clientY - r.top) / r.height)
          }}>
          <img src={p.url} alt={`Resume page ${i + 1}`} className="block w-full" draggable={false} />
          {pins.map((pin, j) => pin.page === i + 1 && (
            <button key={j} type="button" onClick={(e) => { e.stopPropagation(); onSelect?.(j) }}
              style={{ left: `${pin.x * 100}%`, top: `${pin.y * 100}%` }}
              className={cx('absolute grid h-6 w-6 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full text-[11px] font-bold text-white shadow ring-2 ring-white',
                selected === j ? 'scale-125 bg-brand-700' : 'bg-brand-500')}
              aria-label={`Comment ${j + 1}`}>{j + 1}</button>
          ))}
        </div>
      ))}
      {editable && <p className="text-center text-xs text-ink-500">Click anywhere on the page to pin a comment there.</p>}
    </div>
  )
}
