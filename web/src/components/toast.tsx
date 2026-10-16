import { createContext, useCallback, useContext, useState, type ReactNode } from 'react'
import { CheckCircle2, AlertCircle } from 'lucide-react'

interface Toast {
  id: number
  text: string
  tone: 'ok' | 'error'
}

const ToastContext = createContext<(text: string, tone?: Toast['tone']) => void>(() => {})

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const push = useCallback((text: string, tone: Toast['tone'] = 'ok') => {
    const id = Date.now() + Math.random()
    setToasts((t) => [...t, { id, text, tone }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4000)
  }, [])
  return (
    <ToastContext.Provider value={push}>
      {children}
      <div className="pointer-events-none fixed inset-x-0 bottom-4 z-[60] flex flex-col items-center gap-2 px-4" aria-live="polite">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={`pointer-events-auto flex max-w-md items-center gap-2 rounded-xl px-4 py-3 text-sm font-medium text-white shadow-lg ${t.tone === 'ok' ? 'bg-ink-900' : 'bg-red-600'}`}
          >
            {t.tone === 'ok' ? <CheckCircle2 className="h-4 w-4 text-emerald-400" /> : <AlertCircle className="h-4 w-4" />}
            {t.text}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export const useToast = () => useContext(ToastContext)
