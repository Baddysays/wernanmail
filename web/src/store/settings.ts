import { useSyncExternalStore } from 'react'
import type { LocaleCode } from '../i18n'
import i18n from '../i18n'

export type Theme = 'light' | 'dark'
export type FontChoice = 'sans' | 'system' | 'serif' | 'mono'
/** Concrete palette applied to the whole app */
export type MoodId = 'harbor' | 'reef' | 'grove' | 'ember' | 'mist'
/** User choice — `auto` follows time of day */
export type MoodChoice = MoodId | 'auto'

export const MOOD_IDS: MoodId[] = ['harbor', 'reef', 'grove', 'ember', 'mist']

export type Settings = {
  language: LocaleCode
  theme: Theme
  font: FontChoice
  mood: MoodChoice
}

const STORAGE_KEY = 'wernanmail.settings'

const defaultSettings: Settings = {
  language: 'en',
  theme: 'light',
  font: 'sans',
  mood: 'auto',
}

const LEGACY_ACCENT_TO_MOOD: Record<string, MoodId> = {
  teal: 'reef',
  slate: 'harbor',
  amber: 'ember',
}

export function moodForHour(hour: number): MoodId {
  if (hour >= 5 && hour < 10) return 'harbor'
  if (hour >= 10 && hour < 16) return 'reef'
  if (hour >= 16 && hour < 19) return 'grove'
  if (hour >= 19 && hour < 22) return 'ember'
  return 'mist'
}

export function resolveMood(choice: MoodChoice, hour = new Date().getHours()): MoodId {
  if (choice === 'auto') return moodForHour(hour)
  return choice
}

function normalizeSettings(raw: Record<string, unknown>): Settings {
  const base = { ...defaultSettings, ...raw } as Settings & { accent?: string }
  let mood = base.mood
  if (!mood || (mood !== 'auto' && !MOOD_IDS.includes(mood as MoodId))) {
    mood = LEGACY_ACCENT_TO_MOOD[base.accent ?? ''] ?? defaultSettings.mood
  }
  return {
    language: (base.language as LocaleCode) || defaultSettings.language,
    theme: base.theme === 'dark' ? 'dark' : 'light',
    font: (['sans', 'system', 'serif', 'mono'] as FontChoice[]).includes(base.font)
      ? base.font
      : 'sans',
    mood: mood as MoodChoice,
  }
}

function loadSettings(): Settings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return defaultSettings
    return normalizeSettings(JSON.parse(raw) as Record<string, unknown>)
  } catch {
    return defaultSettings
  }
}

let settings: Settings = loadSettings()
const listeners = new Set<() => void>()

function emit() {
  for (const listener of listeners) listener()
}

function persist(next: Settings) {
  settings = next
  localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  applyDocumentSettings(next)
  void i18n.changeLanguage(next.language)
  emit()
}

export function applyDocumentSettings(s: Settings = settings) {
  const root = document.documentElement
  root.dataset.theme = s.theme
  root.dataset.font = s.font
  root.dataset.mood = resolveMood(s.mood)
  delete root.dataset.accent
  root.lang = s.language
}

export function getSettings() {
  return settings
}

export function getEffectiveMood() {
  return resolveMood(settings.mood)
}

export function updateSettings(patch: Partial<Settings>) {
  persist({ ...settings, ...patch })
}

export function subscribeSettings(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useSettings() {
  return useSyncExternalStore(subscribeSettings, getSettings, getSettings)
}

applyDocumentSettings(settings)
void i18n.changeLanguage(settings.language)

// Keep Auto palette in sync with the clock
if (typeof window !== 'undefined') {
  window.setInterval(() => {
    if (settings.mood !== 'auto') return
    const next = resolveMood('auto')
    if (document.documentElement.dataset.mood !== next) {
      applyDocumentSettings(settings)
      emit()
    }
  }, 60_000)
}
