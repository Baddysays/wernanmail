export type FolderId =
  | 'inbox'
  | 'starred'
  | 'sent'
  | 'drafts'
  | 'archive'
  | 'spam'
  | 'trash'

export type Attachment = {
  id: string
  name: string
  size: number
}

export type Message = {
  id: string
  folder: FolderId
  from: { name: string; email: string }
  to: { name: string; email: string }
  subject: string
  preview: string
  body: string
  date: string
  unread: boolean
  starred: boolean
  attachments: Attachment[]
}

export const FOLDERS: { id: FolderId; count?: number }[] = [
  { id: 'inbox', count: 3 },
  { id: 'starred' },
  { id: 'sent' },
  { id: 'drafts', count: 1 },
  { id: 'archive' },
  { id: 'spam', count: 2 },
  { id: 'trash' },
]

export const MOCK_MESSAGES: Message[] = [
  {
    id: '1',
    folder: 'inbox',
    from: { name: 'Melissa Grant', email: 'melissa@studio.example' },
    to: { name: 'Alex Doe', email: 'alex@example.com' },
    subject: 'Project Atlas: Brief and timelines',
    preview:
      'Sharing the latest brief and timeline draft for Atlas. Please review before Thursday.',
    body: `Hi Alex,

Sharing the latest brief and timeline draft for Project Atlas. The design milestones land in mid-May, with engineering kickoff the week after.

Please review the attached files before Thursday — especially the open questions on scope for phase two.

Thanks,
Melissa`,
    date: '2026-05-14T10:24:00',
    unread: true,
    starred: false,
    attachments: [
      { id: 'a1', name: 'Atlas_Brief_v1.2.pdf', size: 1_400_000 },
      { id: 'a2', name: 'Atlas_Timeline.xlsx', size: 892_000 },
    ],
  },
  {
    id: '2',
    folder: 'inbox',
    from: { name: 'Noah Kim', email: 'noah@ops.example' },
    to: { name: 'Alex Doe', email: 'alex@example.com' },
    subject: 'Weekly ops digest',
    preview: 'Uptime held at 99.98%. Two low-priority tickets remain open.',
    body: `Alex,

Weekly ops digest:

• Uptime: 99.98%
• Incidents: none
• Open tickets: 2 (low priority)

Let me know if you want the detailed report.

— Noah`,
    date: '2026-05-14T08:05:00',
    unread: true,
    starred: true,
    attachments: [],
  },
  {
    id: '3',
    folder: 'inbox',
    from: { name: 'Priya Shah', email: 'priya@design.example' },
    to: { name: 'Alex Doe', email: 'alex@example.com' },
    subject: 'Paper Quiet polish notes',
    preview: 'A few density tweaks for the reading pane and sidebar counts.',
    body: `Hi,

Collected polish notes for Paper Quiet:

1. Keep list density airy but readable
2. Teal accent only for primary actions and selection
3. Thin icons throughout

Happy to walk through tomorrow.

Priya`,
    date: '2026-05-13T16:40:00',
    unread: false,
    starred: false,
    attachments: [],
  },
  {
    id: '4',
    folder: 'drafts',
    from: { name: 'Alex Doe', email: 'alex@example.com' },
    to: { name: 'Team', email: 'team@example.com' },
    subject: 'Draft: launch checklist',
    preview: 'Still drafting the pre-launch checklist…',
    body: 'Still drafting the pre-launch checklist…',
    date: '2026-05-12T11:00:00',
    unread: false,
    starred: false,
    attachments: [],
  },
]

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function formatMessageDate(iso: string, locale: string): string {
  const date = new Date(iso)
  const now = new Date()
  const sameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate()

  if (sameDay) {
    return new Intl.DateTimeFormat(locale, {
      hour: 'numeric',
      minute: '2-digit',
    }).format(date)
  }

  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
  }).format(date)
}
