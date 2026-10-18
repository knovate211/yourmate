import { useEffect, useRef } from 'react'
import { EditorView, basicSetup } from 'codemirror'
import { Compartment, EditorState, type Extension } from '@codemirror/state'
import { keymap } from '@codemirror/view'
import { indentWithTab } from '@codemirror/commands'
import { oneDark } from '@codemirror/theme-one-dark'
import { python } from '@codemirror/lang-python'
import { javascript } from '@codemirror/lang-javascript'
import { java } from '@codemirror/lang-java'
import { cpp } from '@codemirror/lang-cpp'
import { yCollab } from 'y-codemirror.next'
import * as Y from 'yjs'
import type { Awareness } from 'y-protocols/awareness'

// The interview room's code editor. The text lives in a shared Yjs document,
// so both people type at once and see each other's cursor.

function languageExtension(lang: string): Extension {
  switch (lang) {
    case 'PYTHON':
      return python()
    case 'JAVASCRIPT':
      return javascript({ typescript: false })
    case 'JAVA':
      return java()
    case 'CPP':
      return cpp()
    default:
      return []
  }
}

export function CodeEditor({ text, awareness, language, readOnly = false, className }: {
  text: Y.Text
  awareness?: Awareness
  language: string
  readOnly?: boolean
  className?: string
}) {
  const host = useRef<HTMLDivElement>(null)
  const view = useRef<EditorView>()
  const lang = useRef(new Compartment())

  useEffect(() => {
    const undo = new Y.UndoManager(text)
    view.current = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: text.toString(),
        extensions: [
          basicSetup,
          keymap.of([indentWithTab]),
          oneDark,
          EditorView.theme({ '&': { height: '100%', fontSize: '13px' }, '.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' } }),
          lang.current.of(languageExtension(language)),
          EditorState.readOnly.of(readOnly),
          yCollab(text, awareness ?? null, { undoManager: undo }),
        ],
      }),
    })
    return () => {
      view.current?.destroy()
      undo.destroy()
    }
    // The editor is created once per document; language changes are applied below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text, awareness, readOnly])

  useEffect(() => {
    view.current?.dispatch({ effects: lang.current.reconfigure(languageExtension(language)) })
  }, [language])

  return <div ref={host} className={className} />
}

/** A read-only view of saved code, for the booking page after the interview. */
export function SavedCode({ code, language }: { code: string; language: string }) {
  const host = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const v = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: code,
        extensions: [basicSetup, oneDark, languageExtension(language), EditorState.readOnly.of(true), EditorView.editable.of(false),
          EditorView.theme({ '&': { maxHeight: '420px', fontSize: '12.5px' } })],
      }),
    })
    return () => v.destroy()
  }, [code, language])
  return <div ref={host} className="overflow-hidden rounded-xl" />
}
