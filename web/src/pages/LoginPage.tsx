import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ApiError, login } from '../api/client'
import styles from './LoginPage.module.css'

type LoginForm = {
  imapHost: string
  username: string
  password: string
}

export function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [form, setForm] = useState<LoginForm>({
    imapHost: 'mail.baddysays.ru',
    username: '',
    password: '',
  })
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

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
      <div className={styles.card}>
        <div className={styles.brand}>
          <span className={styles.brandIcon} aria-hidden>
            ✉
          </span>
          <span className={styles.brandName}>{t('app.name')}</span>
        </div>

        <h1 className={styles.title}>{t('login.title')}</h1>
        <p className={styles.subtitle}>{t('login.subtitle')}</p>

        <form className={styles.form} onSubmit={handleSubmit} noValidate>
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

          {error ? <p className={styles.error}>{error}</p> : null}

          <button type="submit" className={styles.submit} disabled={submitting}>
            {submitting ? t('login.submitting') : t('login.submit')}
          </button>
        </form>

        <p className={styles.hint}>{t('login.hint')}</p>
      </div>
    </div>
  )
}
