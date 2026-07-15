import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
import { useToast } from '../components/Toast/ToastContext'
import styles from './MailPage.module.css'

const ROLE_ORDER = ['inbox', 'sent', 'drafts', 'archive', 'spam', 'trash'] as const
const POLL_MS = 35_000

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
  const { pushToast } = useToast()
  const [folders, setFolders] = useState<Folder[]>([])
  const [folderName, setFolderName] = useState('INBOX')
  const [messages, setMessages] = useState<UiMessage[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [selected, setSelected] = useState<UiMessage | null>(null)
  const [loadingList, setLoadingList] = useState(true)
  const [loadingMsg, setLoadingMsg] = useState(false)
  const [composeOpen, setComposeOpen] = useState(false)
  const [composeDraft, setComposeDraft] = useState<ComposeDraft | null>(null)
  const [searchQuery, setSearchQuery] = useState('')

  const messagesRef = useRef(messages)
  const folderNameRef = useRef(folderName)
  const knownIdsRef = useRef<Set<string>>(new Set())
  const pollReadyRef = useRef(false)

  useEffect(() => {
    messagesRef.current = messages
  }, [messages])

  useEffect(() => {
    folderNameRef.current = folderName
    pollReadyRef.current = false
    knownIdsRef.current = new Set()
  }, [folderName])

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

  const activeRole = useMemo(() => {
    const f = folders.find((x) => x.name === folderName)
    return f ? folderRole(f) : 'other'
  }, [folders, folderName])

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
      pushToast({ tone: 'error', title: t('errors.generic') })
    }
  }, [handleAuthError, pushToast, t])

  const applyMessageList = useCallback(
    (list: UiMessage[], announceNew: boolean) => {
      const ids = new Set(list.map((m) => m.id))
      if (announceNew && pollReadyRef.current) {
        const fresh = list.filter((m) => !knownIdsRef.current.has(m.id) && m.unread)
        if (fresh.length === 1) {
          const msg = fresh[0]
          pushToast({
            tone: 'info',
            title: t('mail.newMail'),
            detail: msg.subject || msg.from.name || msg.from.email,
            durationMs: 5500,
          })
        } else if (fresh.length > 1) {
          pushToast({
            tone: 'info',
            title: t('mail.newMailCount', { count: fresh.length }),
            durationMs: 5500,
          })
        }
      }
      knownIdsRef.current = ids
      pollReadyRef.current = true
      setMessages(list)
      setSelectedId((prev) => {
        if (prev && list.some((m) => m.id === prev)) return prev
        return list[0]?.id ?? null
      })
    },
    [pushToast, t],
  )

  const loadMessages = useCallback(
    async (folder: string, opts?: { silent?: boolean; announceNew?: boolean }) => {
      if (!opts?.silent) setLoadingList(true)
      try {
        const list = await fetchMessages(folder, 50)
        const ui = list.map((m) => summaryToUi(m, folder))
        applyMessageList(ui, Boolean(opts?.announceNew))
        if (!opts?.silent) setSelected(null)
      } catch (err) {
        if (handleAuthError(err)) return
        if (!opts?.silent) {
          pushToast({ tone: 'error', title: t('errors.generic') })
          setMessages([])
          setSelectedId(null)
          setSelected(null)
        }
      } finally {
        if (!opts?.silent) setLoadingList(false)
      }
    },
    [applyMessageList, handleAuthError, pushToast, t],
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
        setFolders((prev) =>
          prev.map((f) => {
            if (f.name !== folderName || !f.unseen) return f
            return { ...f, unseen: Math.max(0, (f.unseen ?? 0) - 1) }
          }),
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

  // Background refresh: folders + current mailbox.
  useEffect(() => {
    const tick = () => {
      if (document.visibilityState === 'hidden') return
      void loadFolders()
      const folder = folderNameRef.current
      if (folder) void loadMessages(folder, { silent: true, announceNew: true })
    }
    const id = window.setInterval(tick, POLL_MS)
    const onVis = () => {
      if (document.visibilityState === 'visible') tick()
    }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      window.clearInterval(id)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [loadFolders, loadMessages])

  const filteredMessages = useMemo(() => {
    const q = searchQuery.trim().toLowerCase()
    if (!q) return messages
    return messages.filter((m) => {
      const hay = `${m.subject} ${m.from.name} ${m.from.email} ${m.to.email} ${m.body || ''}`.toLowerCase()
      return hay.includes(q)
    })
  }, [messages, searchQuery])

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
      pushToast({ tone: 'success', title: t('mail.trashed') })
      await loadMessages(folderName)
      void loadFolders()
    } catch (err) {
      if (handleAuthError(err)) return
      pushToast({ tone: 'error', title: t('errors.generic') })
    }
  }

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null
      const typing =
        target &&
        (target.tagName === 'INPUT' ||
          target.tagName === 'TEXTAREA' ||
          target.isContentEditable)
      if (composeOpen) return
      if (typing && e.key !== 'Escape') {
        if (e.key === 'Escape') (target as HTMLElement).blur()
        return
      }

      if (e.key === '/' && !typing) {
        e.preventDefault()
        document.querySelector<HTMLInputElement>('input[type="search"]')?.focus()
        return
      }
      if (e.key === 'c' && !e.metaKey && !e.ctrlKey && !typing) {
        e.preventDefault()
        openCompose()
        return
      }
      if (e.key === 'r' && selected && !typing) {
        e.preventDefault()
        handleReply(selected)
        return
      }
      if ((e.key === 'j' || e.key === 'k') && !typing) {
        e.preventDefault()
        const list = filteredMessages
        if (!list.length) return
        const idx = list.findIndex((m) => m.id === selectedId)
        const next =
          e.key === 'j'
            ? list[Math.min(list.length - 1, Math.max(0, idx) + 1)]
            : list[Math.max(0, (idx < 0 ? 0 : idx) - 1)]
        if (next) setSelectedId(next.id)
        return
      }
      if ((e.key === 'Delete' || e.key === '#') && selected && !typing) {
        e.preventDefault()
        void handleTrash(selected)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [composeOpen, filteredMessages, openCompose, selected, selectedId, folderName])

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
      pushToast({ tone: 'error', title: t('errors.generic') })
    }
  }

  return (
    <div className={styles.page}>
      <Sidebar
        folders={sidebarFolders}
        activeFolder={folderName}
        onSelectFolder={(name) => setFolderName(name)}
        onCompose={() => openCompose()}
      />
      <MessageList
        messages={filteredMessages}
        selectedId={selectedId}
        loading={loadingList}
        folderRole={activeRole}
        searchQuery={searchQuery}
        onSearchChange={setSearchQuery}
        onSelect={setSelectedId}
        onRefresh={() => {
          void loadMessages(folderName)
          void loadFolders()
        }}
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
          pushToast({
            tone: 'success',
            title: t('mail.sent'),
            detail: `${t('mail.sentSaved')} ${t('mail.sentSpamHint')}`,
            durationMs: 7500,
          })
          if (sentFolder) {
            if (folderName === sentFolder.name) {
              void loadMessages(folderName)
            } else {
              setFolderName(sentFolder.name)
            }
          }
          void loadFolders()
        }}
      />
    </div>
  )
}
