import { forwardRef, useEffect, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { Link } from 'react-router-dom'
import { AlertCircle, CheckCircle2, Loader2, Star, X } from 'lucide-react'
import { errorMessage } from '../lib/graphql'
import { initials } from '../lib/format'

const cx = (...c: (string | false | null | undefined)[]) => c.filter(Boolean).join(' ')
export { cx }

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
const variants: Record<Variant, string> = {
  primary: 'bg-brand-500 text-white hover:bg-brand-600 shadow-sm',
  secondary: 'border border-ink-300 bg-white text-ink-800 hover:bg-ink-50',
  ghost: 'text-ink-700 hover:bg-ink-100',
  danger: 'bg-red-600 text-white hover:bg-red-700 shadow-sm',
}
const sizes = { sm: 'px-3 py-1.5 text-sm', md: 'px-4 py-2 text-sm', lg: 'px-5 py-2.5 text-base' }

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant
  size?: keyof typeof sizes
  loading?: boolean
}

export function Button({ variant = 'primary', size = 'md', loading, disabled, className, children, ...rest }: ButtonProps) {
  return (
    <button
      disabled={disabled || loading}
      className={cx(
        'inline-flex items-center justify-center gap-2 rounded-lg font-semibold transition disabled:cursor-not-allowed disabled:opacity-60',
        variants[variant],
        sizes[size],
        className,
      )}
      {...rest}
    >
      {loading && <Loader2 className="h-4 w-4 animate-spin" />}
      {children}
    </button>
  )
}

export function ButtonLink({ to, variant = 'primary', size = 'md', className, children }: { to: string; variant?: Variant; size?: keyof typeof sizes; className?: string; children: ReactNode }) {
  return (
    <Link to={to} className={cx('inline-flex items-center justify-center gap-2 rounded-lg font-semibold transition', variants[variant], sizes[size], className)}>
      {children}
    </Link>
  )
}

export function Field({ label, hint, error, children, htmlFor }: { label: string; hint?: ReactNode; error?: string; children: ReactNode; htmlFor?: string }) {
  return (
    <div>
      <label className="label" htmlFor={htmlFor}>
        {label}
      </label>
      {children}
      {error ? <p className="mt-1 text-xs text-red-600">{error}</p> : hint ? <p className="mt-1 text-xs text-ink-500">{hint}</p> : null}
    </div>
  )
}

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(function Input({ className, ...rest }, ref) {
  return <input ref={ref} className={cx('input', className)} {...rest} />
})

export function Textarea({ className, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={cx('input min-h-24', className)} {...rest} />
}

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select className={cx('input pr-8', className)} {...rest}>
      {children}
    </select>
  )
}

export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cx('h-5 w-5 animate-spin text-brand-500', className)} />
}

export function PageLoader() {
  return (
    <div className="flex min-h-[40vh] items-center justify-center" role="status" aria-label="Loading">
      <Spinner className="h-7 w-7" />
    </div>
  )
}

export function ErrorNote({ error, className }: { error: unknown; className?: string }) {
  if (!error) return null
  return (
    <div role="alert" className={cx('flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800', className)}>
      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
      <span>{errorMessage(error)}</span>
    </div>
  )
}

export function SuccessNote({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div role="status" className={cx('flex items-start gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800', className)}>
      <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
      <span>{children}</span>
    </div>
  )
}

const badgeTones = {
  gray: 'bg-ink-100 text-ink-700',
  brand: 'bg-brand-100 text-brand-800',
  green: 'bg-emerald-100 text-emerald-800',
  yellow: 'bg-amber-100 text-amber-800',
  red: 'bg-red-100 text-red-800',
  blue: 'bg-sky-100 text-sky-800',
}
export type Tone = keyof typeof badgeTones

export function Badge({ tone = 'gray', children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return <span className={cx('inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-semibold', badgeTones[tone], className)}>{children}</span>
}

export function Avatar({ name, url, size = 40 }: { name: string; url?: string | null; size?: number }) {
  const style = { width: size, height: size, fontSize: size * 0.38 }
  if (url) return <img src={url} alt="" style={style} className="shrink-0 rounded-full object-cover ring-1 ring-ink-200" />
  return (
    <span style={style} aria-hidden className="flex shrink-0 items-center justify-center rounded-full bg-brand-100 font-semibold text-brand-700">
      {initials(name) || '?'}
    </span>
  )
}

export function Stars({ value, size = 14 }: { value: number; size?: number }) {
  return (
    <span className="inline-flex" aria-label={`${value.toFixed(1)} out of 5`}>
      {[1, 2, 3, 4, 5].map((i) => (
        <Star key={i} style={{ width: size, height: size }} className={i <= Math.round(value) ? 'fill-amber-400 text-amber-400' : 'text-ink-300'} />
      ))}
    </span>
  )
}

export function Empty({ icon, title, children, action }: { icon?: ReactNode; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="card flex flex-col items-center px-6 py-12 text-center">
      {icon && <div className="mb-3 text-ink-400">{icon}</div>}
      <h3 className="font-semibold text-ink-900">{title}</h3>
      {children && <p className="mt-1 max-w-md text-sm text-ink-500">{children}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

export function Modal({ open, onClose, title, children, footer }: { open: boolean; onClose: () => void; title: string; children: ReactNode; footer?: ReactNode }) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-ink-900/40 p-0 sm:items-center sm:p-4" onMouseDown={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="max-h-[92vh] w-full overflow-y-auto rounded-t-2xl bg-white shadow-xl sm:max-w-lg sm:rounded-2xl"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-ink-100 px-5 py-4">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button onClick={onClose} className="rounded-lg p-1 text-ink-500 hover:bg-ink-100" aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="px-5 py-4">{children}</div>
        {footer && <div className="flex justify-end gap-2 border-t border-ink-100 px-5 py-3">{footer}</div>}
      </div>
    </div>
  )
}

export function Pagination({ page, pageSize, total, onChange }: { page: number; pageSize: number; total: number; onChange: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / pageSize))
  if (pages <= 1) return null
  return (
    <div className="mt-6 flex items-center justify-center gap-3 text-sm">
      <Button variant="secondary" size="sm" disabled={page <= 1} onClick={() => onChange(page - 1)}>
        Previous
      </Button>
      <span className="text-ink-600">
        Page {page} of {pages}
      </span>
      <Button variant="secondary" size="sm" disabled={page >= pages} onClick={() => onChange(page + 1)}>
        Next
      </Button>
    </div>
  )
}

export function Tabs<T extends string>({ value, onChange, tabs }: { value: T; onChange: (v: T) => void; tabs: { value: T; label: ReactNode }[] }) {
  return (
    <div className="flex gap-1 overflow-x-auto border-b border-ink-200" role="tablist">
      {tabs.map((t) => (
        <button
          key={t.value}
          role="tab"
          aria-selected={value === t.value}
          onClick={() => onChange(t.value)}
          className={cx(
            '-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium transition',
            value === t.value ? 'border-brand-500 text-brand-700' : 'border-transparent text-ink-500 hover:text-ink-800',
          )}
        >
          {t.label}
        </button>
      ))}
    </div>
  )
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h1 className="text-2xl font-bold text-ink-900 sm:text-3xl">{title}</h1>
        {subtitle && <p className="mt-1 text-ink-600">{subtitle}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  )
}
