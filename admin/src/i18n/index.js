import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './locales/en.json'
import ru from './locales/ru.json'
import de from './locales/de.json'
import fr from './locales/fr.json'
import es from './locales/es.json'
import pt from './locales/pt.json'
import zh from './locales/zh.json'
import ja from './locales/ja.json'
import ko from './locales/ko.json'
import it from './locales/it.json'
import pl from './locales/pl.json'
import tr from './locales/tr.json'

export const LOCALES = [
  { code: 'en', label: 'English' },
  { code: 'ru', label: 'Русский' },
  { code: 'de', label: 'Deutsch' },
  { code: 'fr', label: 'Français' },
  { code: 'es', label: 'Español' },
  { code: 'pt', label: 'Português' },
  { code: 'zh', label: '中文' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' },
  { code: 'it', label: 'Italiano' },
  { code: 'pl', label: 'Polski' },
  { code: 'tr', label: 'Türkçe' },
]

const resources = {
  en: { translation: en },
  ru: { translation: ru },
  de: { translation: de },
  fr: { translation: fr },
  es: { translation: es },
  pt: { translation: pt },
  zh: { translation: zh },
  ja: { translation: ja },
  ko: { translation: ko },
  it: { translation: it },
  pl: { translation: pl },
  tr: { translation: tr },
}

const saved = typeof localStorage !== 'undefined' ? localStorage.getItem('wm_admin_lang') : null
const browser = typeof navigator !== 'undefined' ? navigator.language?.slice(0, 2) : 'en'
const supported = LOCALES.map((l) => l.code)
const initial = supported.includes(saved) ? saved : supported.includes(browser) ? browser : 'en'

void i18n.use(initReactI18next).init({
  resources,
  lng: initial,
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

export function setAdminLang(code) {
  localStorage.setItem('wm_admin_lang', code)
  void i18n.changeLanguage(code)
}

export default i18n
