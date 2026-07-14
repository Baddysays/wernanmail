import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { folderRole, type Folder, type FolderRole } from '../../api/types'
import styles from './Sidebar.module.css'

type SidebarProps = {
  folders: Folder[]
  activeFolder: string
  onSelectFolder: (name: string) => void
  onCompose: () => void
}

export function Sidebar({ folders, activeFolder, onSelectFolder, onCompose }: SidebarProps) {
  const { t } = useTranslation()

  return (
    <aside className={styles.sidebar}>
      <button type="button" className={styles.compose} onClick={onCompose}>
        <ComposeIcon />
        {t('nav.compose')}
      </button>

      <nav className={styles.nav} aria-label={t('nav.folders')}>
        {folders.map((folder) => {
          const role = folderRole(folder)
          const active = folder.name === activeFolder
          const label =
            role === 'other'
              ? folder.name
              : t(`nav.${role}`, { defaultValue: folder.name })
          const unseen = folder.unseen ?? 0
          return (
            <button
              key={folder.name}
              type="button"
              className={`${styles.navItem} ${active ? styles.navItemActive : ''} ${role === 'spam' ? styles.navSpam : ''}`}
              onClick={() => onSelectFolder(folder.name)}
            >
              <FolderIcon role={role} />
              <span className={styles.navLabel}>{label}</span>
              {unseen > 0 ? (
                <span className={`${styles.navCount} ${styles.navBadge}`} aria-label={t('mail.unread', { count: unseen })}>
                  {unseen > 99 ? '99+' : unseen}
                </span>
              ) : null}
            </button>
          )
        })}
      </nav>

      <p className={styles.sectionLabel}>{t('nav.settings')}</p>
      <nav className={styles.nav}>
        <Link to="/settings" className={styles.navItem}>
          <SettingsIcon />
          <span className={styles.navLabel}>{t('nav.settings')}</span>
        </Link>
      </nav>
    </aside>
  )
}

function ComposeIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M4 20h4l10.5-10.5a2.1 2.1 0 0 0-3-3L5 17v3z"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function SettingsIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden>
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

function FolderIcon({ role }: { role: FolderRole }) {
  const paths: Record<FolderRole, string> = {
    inbox: 'M4 6h16v12H4V6zm0 0l8 6 8-6',
    starred: 'M12 3.5l2.4 4.9 5.4.8-3.9 3.8.9 5.4L12 16.2 7.2 18.4l.9-5.4L4.2 9.2l5.4-.8L12 3.5z',
    sent: 'M4 12l16-7-7 16-2.2-6.8L4 12z',
    drafts: 'M6 4h9l3 3v13H6V4zm9 0v3h3',
    archive: 'M4 7h16v2H4V7zm2 2v10h12V9',
    spam: 'M12 3l9 16H3L12 3zm0 6v4m0 3h.01',
    trash: 'M9 4h6m-8 3h10l-1 13H8L7 7zm3 3v7m4-7v7',
    other: 'M3 7h6l2 2h10v10H3V7z',
  }

  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d={paths[role]}
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
