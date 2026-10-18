import type { Money } from './types'

const fractionDigits = (currency: string) => {
  try {
    return new Intl.NumberFormat('en', { style: 'currency', currency }).resolvedOptions().maximumFractionDigits ?? 2
  } catch {
    return 2
  }
}

/** Formats an amount in minor units (paise, cents…) for its currency. */
export function formatMoney(m: Money | null | undefined, opts: { free?: string } = {}): string {
  if (!m) return ''
  if (m.amountMinor === 0) return opts.free ?? 'Free'
  const digits = fractionDigits(m.currency)
  const major = m.amountMinor / 10 ** digits
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: m.currency,
      maximumFractionDigits: major % 1 === 0 ? 0 : digits,
    }).format(major)
  } catch {
    return `${m.currency} ${major}`
  }
}

/** Converts a major-unit amount typed by a user into minor units. */
export function toMinor(major: number, currency: string): number {
  return Math.round(major * 10 ** fractionDigits(currency))
}

export function toMajor(minor: number, currency: string): number {
  return minor / 10 ** fractionDigits(currency)
}

export function formatDateTime(iso: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: 'numeric',
    minute: '2-digit',
    timeZone,
  }).format(new Date(iso))
}

export function formatDay(iso: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short', timeZone }).format(new Date(iso))
}

export function formatTime(iso: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit', timeZone }).format(new Date(iso))
}

export function formatDate(iso: string): string {
  return new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(iso))
}

/** A day key (YYYY-MM-DD) for grouping, in a timezone. */
export function dayKey(iso: string, timeZone?: string): string {
  return new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit', timeZone }).format(new Date(iso))
}

export function timeAgo(iso: string): string {
  const s = (Date.now() - new Date(iso).getTime()) / 1000
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  if (s < 60) return 'just now'
  if (s < 3600) return rtf.format(-Math.floor(s / 60), 'minute')
  if (s < 86400) return rtf.format(-Math.floor(s / 3600), 'hour')
  if (s < 86400 * 30) return rtf.format(-Math.floor(s / 86400), 'day')
  return formatDate(iso)
}

export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}

/** Short zone label like "IST" or "GMT+5:30" for a timezone. */
export function zoneLabel(timeZone: string): string {
  const parts = new Intl.DateTimeFormat('en', { timeZoneName: 'short', timeZone }).formatToParts(new Date())
  return parts.find((p) => p.type === 'timeZoneName')?.value ?? timeZone
}

export function minutesToClock(m: number): string {
  const h = Math.floor(m / 60) % 24
  const min = m % 60
  return `${String(h).padStart(2, '0')}:${String(min).padStart(2, '0')}`
}

export function clockToMinutes(s: string): number {
  const [h, m] = s.split(':').map(Number)
  return h * 60 + m
}

// Weekday names in the viewer's language, Sunday first (weekday 0).
export const WEEKDAYS = Array.from({ length: 7 }, (_, i) =>
  new Intl.DateTimeFormat(undefined, { weekday: 'long', timeZone: 'UTC' }).format(new Date(Date.UTC(2024, 0, 7 + i))),
)

export function enumLabel(v: string): string {
  return v.charAt(0) + v.slice(1).toLowerCase().replace(/_/g, ' ')
}

export function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase())
    .join('')
}

export function plural(n: number, word: string, pluralWord = `${word}s`): string {
  return `${n} ${n === 1 ? word : pluralWord}`
}

/** "today 7:30 pm", "tomorrow 9:00 am", or a date. */
export function slotLabel(iso: string) {
  const d = new Date(iso)
  const day = (x: Date) => x.toDateString()
  const today = new Date()
  const tomorrow = new Date(Date.now() + 86_400_000)
  const when = day(d) === day(today) ? 'today' : day(d) === day(tomorrow) ? 'tomorrow' : d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
  return `${when} ${formatTime(iso)}`
}

export function replyLabel(minutes: number) {
  if (minutes < 60) return `~${Math.max(5, Math.round(minutes / 5) * 5)} min`
  if (minutes < 36 * 60) return `~${Math.round(minutes / 60)} h`
  return `~${Math.round(minutes / 1440)} days`
}
