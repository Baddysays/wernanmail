import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ApiError, fetchFolders, fetchMessage, fetchMessages } from '../api/client'
import {
  detailToUi,
  folderRole,
  summaryToUi,
  type Folder,
  type UiMessage,
} from '../api/types'
import { Sidebar } from '../components/Layout/Sidebar'
import { MessageList } from '../components/Layout/MessageList'
import { ReadingPane } from '../components/Layout/ReadingPane'
import styles from './MailPage.module.css'

const ROLE_ORDER = ['inbox', 'sent', 'drafts', 'archive', 'spam', 'trash'] as const

export function MailPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [folders, setFolders] = useState<Folder[]>([])
  const [folderName, setFolderName] = useState('INBOX')
  const [messages, setMessages] = useState<UiMessage[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [selected, setSelected] = useState<UiMessage | null>(null)
  const [loadingList, setLoadingList] = useState(true)
  const [loadingMsg, setLoadingMsg] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const sidebarFolders = useMemo(() => {
    const ranked = [...folders].sort((a, b) => {
      const ra = ROLE_ORDER.indexOf(folderRole(a) as (typeof ROLE_ORDER)[number])
      const rb = ROLE_ORDER.indexOf(folderRole(b) as (typeof ROLE_ORDER)[number])
      const ia = ra === -1 ? 99 : ra
      const ib = rb === -1 ? 99 : rb
      if (ia !== ib) return ia - ib
      return a.name.localeCompare(b.name)
    })
    // Prefer one folder per standard role for the main nav
    const seen = new Set<string>()
    const primary: Folder[] = []
    for (const f of ranked) {
      const role = folderRole(f)
      if (role === 'other') continue
      if (seen.has(role)) continue
      seen.add(role)
      primary.push(f)
    }
    if (primary.length === 0 && folders.length > 0) {
      return folders.slice(0, 8)
    }
    return primary
  }, [folders])

  const handleAuthError = useCallback(
    (err: unknown) => {
      if (err instanceof ApiError && (err.status === 401 || err.code.startsWith('mail.session'))) {
        navigate('/login')
        return true
      }
      return false
    },
    [navigate],
  )

  const loadFolders = useCallback(async () => {
    try {
      const list = await fetchFolders()
      setFolders(list)
      const inbox =
        list.find((f) => folderRole(f) === 'inbox') ??
        list.find((f) => f.name.toUpperCase() === 'INBOX') ??
        list[0]
      if (inbox) setFolderName(inbox.name)
    } catch (err) {
      if (handleAuthError(err)) return
      setError(t('errors.generic'))
    }
  }, [handleAuthError, t])

  const loadMessages = useCallback(
    async (folder: string) => {
      setLoadingList(true)
      setError(null)
      try {
        const list = await fetchMessages(folder, 50)
        const ui = list.map((m) => summaryToUi(m, folder))
        setMessages(ui)
        setSelectedId(ui[0]?.id ?? null)
        setSelected(null)
      } catch (err) {
        if (handleAuthError(err)) return
        setError(t('errors.generic'))
        setMessages([])
        setSelectedId(null)
        setSelected(null)
      } finally {
        setLoadingList(false)
      }
    },
    [handleAuthError, t],
  )

  useEffect(() => {
    void loadFolders()
  }, [loadFolders])

  useEffect(() => {
    if (!folderName) return
    void loadMessages(folderName)
  }, [folderName, loadMessages])

  useEffect(() => {
    if (!selectedId) {
      setSelected(null)
      return
    }
    let cancelled = false
    setLoadingMsg(true)
    void fetchMessage(selectedId, folderName)
      .then((detail) => {
        if (cancelled) return
        setSelected(detailToUi(detail, folderName))
      })
      .catch((err) => {
        if (cancelled) return
        if (handleAuthError(err)) return
        // Keep list row visible even if body fetch fails
        const row = messages.find((m) => m.id === selectedId) ?? null
        setSelected(row)
      })
      .finally(() => {
        if (!cancelled) setLoadingMsg(false)
      })
    return () => {
      cancelled = true
    }
  }, [selectedId, folderName, handleAuthError, messages])

  return (
    <div className={styles.page}>
      <Sidebar
        folders={sidebarFolders}
        activeFolder={folderName}
        onSelectFolder={(name) => setFolderName(name)}
      />
      {error ? <div className={styles.errorBanner}>{error}</div> : null}
      <MessageList
        messages={messages}
        selectedId={selectedId}
        loading={loadingList}
        onSelect={setSelectedId}
        onRefresh={() => void loadMessages(folderName)}
      />
      <ReadingPane message={selected} loading={loadingMsg} />
    </div>
  )
}
