import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ApiError, login } from '../api/client'
import {
  MOOD_IDS,
  resolveMood,
  updateSettings,
  useSettings,
  type MoodId,
} from '../store/settings'
import styles from './LoginPage.module.css'

type LoginForm = {
  imapHost: string
  username: string
  password: string
}

export function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const settings = useSettings()
  const effectiveMood = resolveMood(settings.mood)
  const [form, setForm] = useState<LoginForm>({
    imapHost: 'mail.baddysays.ru',
    username: '',
    password: '',
  })
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function selectMood(id: MoodId) {
    updateSettings({ mood: id })
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)

    if (!form.imapHost.trim() || !form.username.trim() || !form.password) {
      setError(t('errors.required'))
      return
    }

    setSubmitting(true)
    try {
      const host = form.imapHost.trim()
      await login({
        imapHost: host,
        imapPort: 993,
        smtpHost: host,
        smtpPort: 587,
        username: form.username.trim(),
        password: form.password,
        tls: true,
      })
      sessionStorage.removeItem('wernanmail.demo')
      navigate('/mail')
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'mail.auth_failed') {
          setError(t('errors.invalidCredentials'))
        } else if (err.code === 'mail.invalid_request') {
          setError(t('errors.required'))
        } else {
          setError(t(`errors.codes.${err.code}`, { defaultValue: t('errors.generic') }))
        }
      } else {
        setError(t('errors.network'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className={styles.page}>
      <aside className={styles.hero} aria-label={t('app.name')}>
        <div className={styles.aurora} aria-hidden>
          <span className={styles.blobA} />
          <span className={styles.blobB} />
          <span className={styles.blobC} />
        </div>
        <div className={styles.heroGrain} aria-hidden />
        <div className={styles.heroContent}>
          <div className={styles.mark}>
            <svg viewBox="0 0 32 32" width="28" height="28" aria-hidden>
              <rect width="32" height="32" rx="8" fill="currentColor" opacity="0.14" />
              <path
                d="M7 11.2c0-.66.54-1.2 1.2-1.2h15.6c.66 0 1.2.54 1.2 1.2v9.6c0 .66-.54 1.2-1.2 1.2H8.2c-.66 0-1.2-.54-1.2-1.2v-9.6zm2.1.55 6.9 4.55 6.9-4.55"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.6"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          </div>
          <p className={styles.brand}>{t('app.name')}</p>
          <p className={styles.tagline}>{t('login.tagline')}</p>

          <div className={styles.moods} role="radiogroup" aria-label={t('settings.mood')}>
            {MOOD_IDS.map((id) => (
              <button
                key={id}
                type="button"
                role="radio"
                aria-checked={effectiveMood === id}
                aria-label={t(`settings.moods.${id}`)}
                className={`${styles.moodDot} ${effectiveMood === id ? styles.moodDotActive : ''}`}
                data-mood-swatch={id}
                onClick={() => selectMood(id)}
              />
            ))}
          </div>
          {settings.mood === 'auto' ? (
            <p className={styles.moodHint}>{t('settings.moodAutoActive')}</p>
          ) : null}
        </div>
      </aside>

      <main className={styles.panel}>
        <div className={styles.panelInner}>
          <h1 className={styles.title}>{t('login.title')}</h1>
          <p className={styles.subtitle}>{t('login.subtitle')}</p>

          <form className={styles.form} onSubmit={handleSubmit} noValidate>
            <div className={styles.field}>
              <label htmlFor="username">{t('login.username')}</label>
              <input
                id="username"
                name="username"
                autoComplete="username"
                placeholder={t('login.usernamePlaceholder')}
                value={form.username}
                onChange={(e) => setForm((f) => ({ ...f, username: e.target.value }))}
                required
                autoFocus
              />
            </div>

            <div className={styles.field}>
              <label htmlFor="password">{t('login.password')}</label>
              <input
                id="password"
                name="password"
                type="password"
                autoComplete="current-password"
                placeholder={t('login.passwordPlaceholder')}
                value={form.password}
                onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
                required
              />
            </div>

            <details className={styles.advanced}>
              <summary>{t('login.server')}</summary>
              <div className={styles.field}>
                <label htmlFor="imapHost">{t('login.host')}</label>
                <input
                  id="imapHost"
                  name="imapHost"
                  autoComplete="url"
                  placeholder={t('login.hostPlaceholder')}
                  value={form.imapHost}
                  onChange={(e) => setForm((f) => ({ ...f, imapHost: e.target.value }))}
                  required
                />
              </div>
            </details>

            {error ? <p className={styles.error}>{error}</p> : null}

            <button type="submit" className={styles.submit} disabled={submitting}>
              {submitting ? t('login.submitting') : t('login.submit')}
            </button>
          </form>

          <p className={styles.hint}>{t('login.hint')}</p>
        </div>
      </main>
    </div>
  )
}
