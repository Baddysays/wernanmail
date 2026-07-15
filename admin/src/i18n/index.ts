import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './locales/en.json'
import ru from './locales/ru.json'

export const LOCALES = [
  { code: 'en', label: 'English' },
  { code: 'ru', label: 'Русский' },
] as const

export type LocaleCode = (typeof LOCALES)[number]['code']

const resources = {
  en: { translation: en },
  ru: { translation: ru },
}

const saved = typeof localStorage !== 'undefined' ? localStorage.getItem('wm_admin_lang') : null
const browser = typeof navigator !== 'undefined' ? navigator.language?.slice(0, 2) : 'en'
const supported = LOCALES.map((l) => l.code) as string[]
const initial = supported.includes(saved ?? '')
  ? (saved as string)
  : supported.includes(browser ?? '')
    ? (browser as string)
    : 'en'

void i18n.use(initReactI18next).init({
  resources,
  lng: initial,
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

export function setAdminLang(code: string) {
  localStorage.setItem('wm_admin_lang', code)
  void i18n.changeLanguage(code)
}

export default i18n
