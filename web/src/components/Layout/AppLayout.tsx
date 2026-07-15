import { useEffect, useState } from 'react'
import { Link, Outlet, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { fetchMe, logout, IMPERSONATE_KEY } from '../../api/client'
import styles from './AppLayout.module.css'

type ImpersonateInfo = { username: string; impersonatedBy: string }

function readImpersonate(): ImpersonateInfo | null {
  try {
    const raw = sessionStorage.getItem(IMPERSONATE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as ImpersonateInfo
    if (!parsed?.username) return null
    return parsed
  } catch {
    return null
  }
}

export function AppLayout() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [impersonate, setImpersonate] = useState<ImpersonateInfo | null>(() => readImpersonate())

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const me = await fetchMe()
        if (cancelled) return
        if (me.impersonated) {
          const info = {
            username: me.username,
            impersonatedBy: me.impersonatedBy || '',
          }
          sessionStorage.setItem(IMPERSONATE_KEY, JSON.stringify(info))
          setImpersonate(info)
        } else {
          sessionStorage.removeItem(IMPERSONATE_KEY)
          setImpersonate(null)
        }
      } catch {
        /* not signed in yet — route guards handle it */
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  async function handleLogout() {
    try {
      await logout()
    } catch {
      /* session may already be gone */
    }
    sessionStorage.removeItem(IMPERSONATE_KEY)
    navigate('/login')
  }

  return (
    <div className={styles.shell}>
      {impersonate ? (
        <div className={styles.suBanner} role="status">
          <span>
            {t('impersonate.banner', {
              user: impersonate.username,
              admin: impersonate.impersonatedBy || t('impersonate.admin'),
            })}
          </span>
          <button type="button" className={styles.suExit} onClick={() => void handleLogout()}>
            {t('impersonate.exit')}
          </button>
        </div>
      ) : null}
      <header className={styles.header}>
        <Link to="/mail" className={styles.brand}>
          <span className={styles.brandIcon} aria-hidden>
            <MailIcon />
          </span>
          <span className={styles.brandName}>{t('app.name')}</span>
        </Link>

        <div className={styles.headerSpacer} aria-hidden />

        <div className={styles.headerActions}>
          <Link
            to="/settings"
            className={styles.iconBtn}
            aria-label={t('nav.settings')}
            title={t('nav.settings')}
          >
            <GearIcon />
          </Link>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('nav.logout')}
            title={t('nav.logout')}
            onClick={() => void handleLogout()}
          >
            <LogoutIcon />
          </button>
        </div>
      </header>

      <Outlet />
    </div>
  )
}

function MailIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M3 7.5A2.5 2.5 0 0 1 5.5 5h13A2.5 2.5 0 0 1 21 7.5v9a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 16.5v-9z"
        stroke="currentColor"
        strokeWidth="1.7"
      />
      <path
        d="M4 7l8 6 8-6"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function GearIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
      <circle cx="12" cy="12" r="3" stroke="currentColor" strokeWidth="1.6" />
      <path
        d="M12 3v2.2M12 18.8V21M4.9 4.9l1.6 1.6M17.5 17.5l1.6 1.6M3 12h2.2M18.8 12H21M4.9 19.1l1.6-1.6M17.5 6.5l1.6-1.6"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
      />
    </svg>
  )
}

function LogoutIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M10 4H6.5A2.5 2.5 0 0 0 4 6.5v11A2.5 2.5 0 0 0 6.5 20H10"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
      />
      <path
        d="M14 8l4 4-4 4M10 12h8"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
