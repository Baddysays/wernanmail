import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  ApiError,
  fetchFolders,
  fetchMessage,
  fetchMessages,
  trashMessage,
  updateMessageFlags,
} from '../api/client'
import {
  detailToUi,
  folderRole,
  summaryToUi,
  type Folder,
  type UiMessage,
} from '../api/types'
import { ComposeDialog, type ComposeDraft } from '../components/Compose/ComposeDialog'
import { Sidebar } from '../components/Layout/Sidebar'
import { MessageList } from '../components/Layout/MessageList'
import { ReadingPane } from '../components/Layout/ReadingPane'
import styles from './MailPage.module.css'

const ROLE_ORDER = ['inbox', 'sent', 'drafts', 'archive', 'spam', 'trash'] as const

function withRePrefix(subject: string, prefix: 'Re' | 'Fwd') {
  const trimmed = subject.trim()
  if (!trimmed) return `${prefix}:`
  const re = new RegExp(`^${prefix}:\\s*`, 'i')
  if (re.test(trimmed)) return trimmed
  return `${prefix}: ${trimmed}`
}

function quoteBody(message: UiMessage) {
  const lines = (message.body || '').split('\n').map((line) => `> ${line}`)
  return `\n\nOn ${message.date}, ${message.from.name || message.from.email} wrote:\n${lines.join('\n')}`
}

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
  const [notice, setNotice] = useState<string | null>(null)
  const [composeOpen, setComposeOpen] = useState(false)
  const [composeDraft, setComposeDraft] = useState<ComposeDraft | null>(null)

  const sidebarFolders = useMemo(() => {
    const ranked = [...folders].sort((a, b) => {
      const ra = ROLE_ORDER.indexOf(folderRole(a) as (typeof ROLE_ORDER)[number])
      const rb = ROLE_ORDER.indexOf(folderRole(b) as (typeof ROLE_ORDER)[number])
      const ia = ra === -1 ? 99 : ra
      const ib = rb === -1 ? 99 : rb
      if (ia !== ib) return ia - ib
      return a.name.localeCompare(b.name)
    })
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

  const sentFolder = useMemo(
    () => sidebarFolders.find((f) => folderRole(f) === 'sent') ?? null,
    [sidebarFolders],
  )

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

  const openCompose = useCallback((draft?: ComposeDraft) => {
    setComposeDraft(draft ?? null)
    setComposeOpen(true)
  }, [])

  const closeCompose = useCallback(() => {
    setComposeOpen(false)
    setComposeDraft(null)
  }, [])

  const loadFolders = useCallback(async () => {
    try {
      const list = await fetchFolders()
      setFolders(list)
      setFolderName((prev) => {
        if (prev) return prev
        const inbox =
          list.find((f) => folderRole(f) === 'inbox') ??
          list.find((f) => f.name.toUpperCase() === 'INBOX') ??
          list[0]
        return inbox?.name ?? 'INBOX'
      })
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
        setSelectedId((prev) => {
          if (prev && ui.some((m) => m.id === prev)) return prev
          return ui[0]?.id ?? null
        })
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
        const ui = detailToUi(detail, folderName)
        setSelected(ui)
        setMessages((prev) =>
          prev.map((m) =>
            m.id === selectedId ? { ...m, unread: false, preview: ui.preview || m.preview } : m,
          ),
        )
      })
      .catch((err) => {
        if (cancelled) return
        if (handleAuthError(err)) return
        setSelected(null)
      })
      .finally(() => {
        if (!cancelled) setLoadingMsg(false)
      })
    return () => {
      cancelled = true
    }
  }, [selectedId, folderName, handleAuthError])

  function handleReply(message: UiMessage) {
    openCompose({
      to: message.from.email,
      subject: withRePrefix(message.subject || '', 'Re'),
      body: quoteBody(message),
    })
  }

  function handleReplyAll(message: UiMessage) {
    const others = [message.to.email].filter(
      (e) => e && e.toLowerCase() !== message.from.email.toLowerCase(),
    )
    openCompose({
      to: message.from.email,
      cc: others.join(', '),
      subject: withRePrefix(message.subject || '', 'Re'),
      body: quoteBody(message),
    })
  }

  function handleForward(message: UiMessage) {
    openCompose({
      subject: withRePrefix(message.subject || '', 'Fwd'),
      body: `\n\n---------- Forwarded message ----------\nFrom: ${message.from.name || message.from.email} <${message.from.email}>\nDate: ${message.date}\nSubject: ${message.subject || ''}\nTo: ${message.to.email}\n\n${message.body || ''}`,
    })
  }

  async function handleTrash(message: UiMessage) {
    try {
      await trashMessage(message.id, message.folder || folderName)
      setNotice(t('mail.trashed'))
      await loadMessages(folderName)
    } catch (err) {
      if (handleAuthError(err)) return
      setError(t('errors.generic'))
    }
  }

  async function handleToggleStar(message: UiMessage) {
    const next = !message.starred
    try {
      await updateMessageFlags(message.id, message.folder || folderName, {
        add: next ? ['\\Flagged'] : [],
        remove: next ? [] : ['\\Flagged'],
      })
      setMessages((prev) =>
        prev.map((m) => (m.id === message.id ? { ...m, starred: next } : m)),
      )
      setSelected((cur) => (cur?.id === message.id ? { ...cur, starred: next } : cur))
    } catch (err) {
      if (handleAuthError(err)) return
      setError(t('errors.generic'))
    }
  }

  useEffect(() => {
    if (!notice) return
    const tmr = window.setTimeout(() => setNotice(null), 3200)
    return () => window.clearTimeout(tmr)
  }, [notice])

  return (
    <div className={styles.page}>
      <Sidebar
        folders={sidebarFolders}
        activeFolder={folderName}
        onSelectFolder={(name) => setFolderName(name)}
        onCompose={() => openCompose()}
      />
      {error ? <div className={styles.errorBanner}>{error}</div> : null}
      {notice ? <div className={styles.noticeBanner}>{notice}</div> : null}
      <MessageList
        messages={messages}
        selectedId={selectedId}
        loading={loadingList}
        onSelect={setSelectedId}
        onRefresh={() => void loadMessages(folderName)}
        onToggleStar={(id) => {
          const msg = messages.find((m) => m.id === id)
          if (msg) void handleToggleStar(msg)
        }}
        onTrashSelected={() => {
          const msg = messages.find((m) => m.id === selectedId) ?? selected
          if (msg) void handleTrash(msg)
        }}
      />
      <ReadingPane
        message={selected}
        loading={loadingMsg}
        onReply={handleReply}
        onReplyAll={handleReplyAll}
        onForward={handleForward}
        onTrash={handleTrash}
        onToggleStar={handleToggleStar}
      />
      <ComposeDialog
        open={composeOpen}
        draft={composeDraft}
        onClose={closeCompose}
        onSent={() => {
          setNotice(t('mail.sent'))
          if (sentFolder) {
            if (folderName === sentFolder.name) {
              void loadMessages(folderName)
            } else {
              setFolderName(sentFolder.name)
            }
          }
        }}
      />
    </div>
  )
}
