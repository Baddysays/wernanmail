import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { LOCALES, type LocaleCode } from '../i18n'
import {
  MOOD_IDS,
  resolveMood,
  updateSettings,
  useSettings,
  type FontChoice,
  type MoodChoice,
  type MoodId,
  type Theme,
} from '../store/settings'
import styles from './SettingsPage.module.css'

const MOOD_PREVIEW: Record<MoodId, string> = {
  harbor: 'linear-gradient(135deg, #0f2438, #3d7ea6)',
  reef: 'linear-gradient(135deg, #143c48, #4aa89a)',
  grove: 'linear-gradient(135deg, #1a2e1c, #6a9a4e)',
  ember: 'linear-gradient(135deg, #1c1410, #c47a3a)',
  mist: 'linear-gradient(135deg, #12151a, #6b7c8f)',
}

export function SettingsPage() {
  const { t } = useTranslation()
  const settings = useSettings()
  const effective = resolveMood(settings.mood)

  return (
    <div className={styles.page}>
      <Link to="/mail" className={styles.back}>
        ← {t('settings.backToMail')}
      </Link>

      <h1 className={styles.title}>{t('settings.title')}</h1>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>{t('settings.mood')}</h2>
        <p className={styles.sectionHint}>{t('settings.moodHint')}</p>
        <div className={styles.moodGrid}>
          <button
            type="button"
            className={`${styles.moodCard} ${settings.mood === 'auto' ? styles.moodCardActive : ''}`}
            onClick={() => updateSettings({ mood: 'auto' })}
          >
            <span className={`${styles.moodPreview} ${styles.moodPreviewAuto}`} aria-hidden />
            <span className={styles.moodName}>{t('settings.moods.auto')}</span>
            <span className={styles.moodMeta}>
              {t('settings.moodAutoMeta', { mood: t(`settings.moods.${effective}`) })}
            </span>
          </button>

          {MOOD_IDS.map((id) => (
            <button
              key={id}
              type="button"
              className={`${styles.moodCard} ${settings.mood === id ? styles.moodCardActive : ''}`}
              onClick={() => updateSettings({ mood: id as MoodChoice })}
            >
              <span
                className={styles.moodPreview}
                style={{ background: MOOD_PREVIEW[id] }}
                aria-hidden
              />
              <span className={styles.moodName}>{t(`settings.moods.${id}`)}</span>
            </button>
          ))}
        </div>
      </section>

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
    </div>
  )
}
