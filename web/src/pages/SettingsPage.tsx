import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { LOCALES, type LocaleCode } from '../i18n'
import {
  updateSettings,
  useSettings,
  type AccentChoice,
  type FontChoice,
  type Theme,
} from '../store/settings'
import styles from './SettingsPage.module.css'

const ACCENT_PREVIEW: Record<AccentChoice, string> = {
  teal: '#2a8f80',
  slate: '#516b80',
  amber: '#c47d22',
}

export function SettingsPage() {
  const { t } = useTranslation()
  const settings = useSettings()

  return (
    <div className={styles.page}>
      <Link to="/mail" className={styles.back}>
        ← {t('settings.backToMail')}
      </Link>

      <h1 className={styles.title}>{t('settings.title')}</h1>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>{t('settings.language')}</h2>
        <p className={styles.sectionHint}>{t('settings.languageHint')}</p>
        <select
          className={styles.select}
          value={settings.language}
          onChange={(e) =>
            updateSettings({ language: e.target.value as LocaleCode })
          }
          aria-label={t('settings.language')}
        >
          {LOCALES.map((locale) => (
            <option key={locale.code} value={locale.code}>
              {locale.label}
            </option>
          ))}
        </select>
      </section>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>{t('settings.theme')}</h2>
        <p className={styles.sectionHint}>{t('settings.themeHint')}</p>
        <div className={styles.control}>
          {(['light', 'dark'] as Theme[]).map((theme) => (
            <button
              key={theme}
              type="button"
              className={`${styles.choice} ${settings.theme === theme ? styles.choiceActive : ''}`}
              onClick={() => updateSettings({ theme })}
            >
              {theme === 'light' ? t('settings.themeLight') : t('settings.themeDark')}
            </button>
          ))}
        </div>
      </section>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>{t('settings.font')}</h2>
        <p className={styles.sectionHint}>{t('settings.fontHint')}</p>
        <div className={styles.control}>
          {(
            [
              ['sans', 'fontSans'],
              ['system', 'fontSystem'],
              ['serif', 'fontSerif'],
              ['mono', 'fontMono'],
            ] as const
          ).map(([value, labelKey]) => (
            <button
              key={value}
              type="button"
              className={`${styles.choice} ${settings.font === value ? styles.choiceActive : ''}`}
              onClick={() => updateSettings({ font: value as FontChoice })}
            >
              {t(`settings.${labelKey}`)}
            </button>
          ))}
        </div>
      </section>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>{t('settings.accent')}</h2>
        <p className={styles.sectionHint}>{t('settings.accentHint')}</p>
        <div className={styles.swatchRow}>
          {(
            [
              ['teal', 'accentTeal'],
              ['slate', 'accentSlate'],
              ['amber', 'accentAmber'],
            ] as const
          ).map(([value, labelKey]) => (
            <button
              key={value}
              type="button"
              className={`${styles.swatch} ${settings.accent === value ? styles.swatchActive : ''}`}
              onClick={() => updateSettings({ accent: value as AccentChoice })}
            >
              <span
                className={styles.swatchDot}
                style={{ background: ACCENT_PREVIEW[value] }}
              />
              <span className={styles.swatchLabel}>{t(`settings.${labelKey}`)}</span>
            </button>
          ))}
        </div>
      </section>
    </div>
  )
}
