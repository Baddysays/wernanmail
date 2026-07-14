import { useMemo, useState } from 'react'
import type { FolderId } from '../data/mockMail'
import { MOCK_MESSAGES } from '../data/mockMail'
import { Sidebar } from '../components/Layout/Sidebar'
import { MessageList } from '../components/Layout/MessageList'
import { ReadingPane } from '../components/Layout/ReadingPane'
import styles from './MailPage.module.css'

export function MailPage() {
  const [folder, setFolder] = useState<FolderId>('inbox')
  const [selectedId, setSelectedId] = useState<string | null>('1')

  const messages = useMemo(
    () => MOCK_MESSAGES.filter((m) => m.folder === folder),
    [folder],
  )

  const selected = messages.find((m) => m.id === selectedId) ?? null

  function handleSelectFolder(id: FolderId) {
    setFolder(id)
    const first = MOCK_MESSAGES.find((m) => m.folder === id)
    setSelectedId(first?.id ?? null)
  }

  return (
    <div className={styles.page}>
      <Sidebar activeFolder={folder} onSelectFolder={handleSelectFolder} />
      <MessageList
        messages={messages}
        selectedId={selectedId}
        onSelect={setSelectedId}
      />
      <ReadingPane message={selected} />
    </div>
  )
}
