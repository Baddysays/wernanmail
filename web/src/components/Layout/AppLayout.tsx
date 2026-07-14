import { Link, Outlet } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import styles from './AppLayout.module.css'

export function AppLayout() {
  const { t } = useTranslation()

  return (
    <div className={styles.shell}>
      <header className={styles.header}>
        <Link to="/mail" className={styles.brand}>
          <span className={styles.brandIcon} aria-hidden>
            <MailIcon />
          </span>
          <span className={styles.brandName}>{t('app.name')}</span>
        </Link>

        <label className={styles.search}>
          <SearchIcon />
          <input type="search" placeholder={t('nav.search')} />
          <kbd className={styles.searchHint}>/</kbd>
        </label>

        <div className={styles.headerActions}>
          <Link
            to="/settings"
            className={styles.iconBtn}
            aria-label={t('nav.settings')}
            title={t('nav.settings')}
          >
            <GearIcon />
          </Link>
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

function SearchIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden>
      <circle cx="11" cy="11" r="6.5" stroke="currentColor" strokeWidth="1.6" />
      <path d="M16 16l4 4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
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
