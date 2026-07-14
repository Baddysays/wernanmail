import { useSyncExternalStore } from 'react'
import type { LocaleCode } from '../i18n'
import i18n from '../i18n'

export type Theme = 'light' | 'dark'
export type FontChoice = 'sans' | 'system' | 'serif' | 'mono'
export type AccentChoice = 'teal' | 'slate' | 'amber'

export type Settings = {
  language: LocaleCode
  theme: Theme
  font: FontChoice
  accent: AccentChoice
}

const STORAGE_KEY = 'wernanmail.settings'

const defaultSettings: Settings = {
  language: 'en',
  theme: 'light',
  font: 'sans',
  accent: 'teal',
}

function loadSettings(): Settings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return defaultSettings
    return { ...defaultSettings, ...JSON.parse(raw) } as Settings
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
  root.dataset.accent = s.accent
  root.lang = s.language
}

export function getSettings() {
  return settings
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

// Apply on module load
applyDocumentSettings(settings)
void i18n.changeLanguage(settings.language)
