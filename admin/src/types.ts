export type NavTab =
  | 'overview'
  | 'domains'
  | 'mailboxes'
  | 'queue'
  | 'quarantine'
  | 'settings'
  | 'backup'
  | 'audit'

export type SettingType = 'text' | 'number' | 'bool' | 'textarea'

export type SettingField = {
  key: string
  type: SettingType
  hint?: string
}

export type SettingGroup = {
  id: string
  fields: SettingField[]
}

export type AdminCreds = {
  token: string
  username?: string
  user?: string
}

export type Domain = {
  id: string | number
  name: string
  enabled?: boolean
  catchAll?: string
  defaultQuotaBytes?: number
  dkimSelector?: string
  dkimPublic?: string
  mailboxCount?: number
  // legacy JSON shapes from older admin API payloads
  Name?: string
  ID?: string
  DKIMSelector?: string
  DKIMPublic?: string
}

export type Mailbox = {
  id: string | number
  localPart: string
  displayName?: string
  enabled?: boolean
  quotaBytes?: number
  usedBytes?: number
  domainId?: string | number
  createdAt?: string
}

export type MailFilter = {
  id?: string | number
  mailboxId?: string | number
  enabled: boolean
  priority: number
  matchField: 'from' | 'subject' | 'to'
  matchOp: 'contains' | 'equals'
  matchValue: string
  action: 'fileinto' | 'reject' | 'flag_spam'
  actionArg: string
}

export type Alias = {
  id: string | number
  localPart: string
  mailboxId: string | number
}

export type QueueJob = {
  id: string | number
  kind: string
  attempts: number
  maxAttempts: number
  lastError?: string
  nextAt?: string
  createdAt?: string
  updatedAt?: string
  payloadJson?: string
}

export type SpamReason = {
  code?: string
  detail?: string
  score?: number
}

export type QuarantineItem = {
  id: string | number
  subject?: string
  from?: string
  fromAddr?: string
  verdictJson?: string
  score?: number
  reason?: string
  createdAt?: string
  mailboxId?: string | number
  mailboxAddr?: string
}

export type AuditEntry = {
  id?: string | number
  action?: string
  actor?: string
  detail?: string
  at?: string
  createdAt?: string
  target?: string
}

export type Dashboard = {
  queuePending?: number
  queueDead?: number
  quarantineCount?: number
  domainCount?: number
  mailboxCount?: number
  status?: string
  attention?: boolean
  quarantine?: number
  domains?: number
}

export type DnsCheck = {
  state?: string
  detail?: string
}

export type DnsStatus = {
  domain?: string
  mx?: DnsCheck
  spf?: DnsCheck
  dkim?: DnsCheck
  dmarc?: DnsCheck
}

export type DmarcReport = {
  id?: string | number
  ip?: string
  sourceIp?: string
  count?: number
  messageCount?: number
  dkim?: string
  spf?: string
}

export type HostProcess = {
  name: string
  pid: number
  rssBytes: number
  cpuPercent?: number
}

export type HostStats = {
  mailRssBytes?: number
  mailCpuPercent?: number
  dataBytes?: number
  binBytes?: number
  mem?: { totalBytes?: number; usedBytes?: number }
  disk?: { freeBytes?: number; totalBytes?: number }
  processes?: HostProcess[]
}

export type OpsStatus = {
  defaultQuotaBytes?: number
  quarantineAt?: number | string
  rejectAt?: number | string
  antivirus?: boolean
  relay?: string
  greylist?: boolean
  bounce?: boolean
  tlsCerts?: boolean
  sendPerHour?: number | string
  greylistSeconds?: number
  bounceEnabled?: boolean
  tlsConfigured?: boolean
  rateSendPerHour?: number | string
  schemaVersion?: number
}

export type PostureCheck = {
  state?: string
  detail?: string
}

export type Posture = {
  status?: string
  ip?: string
  ipSource?: string
  ehlo?: string
  ptr?: PostureCheck
  rbl?: PostureCheck
  antispam?: {
    state?: string
    detail?: string
    rbls?: string[]
    flagAt?: number
    quarantineAt?: number
    rejectAt?: number
    greylistSeconds?: number
    probe?: {
      ok?: boolean
      clean?: { score?: number; action?: string }
      spammy?: { score?: number; action?: string }
    }
  }
  stack?: {
    state?: string
    mode?: string
    expected?: string[]
    running?: string[]
    missing?: string[]
  }
  queue?: {
    state?: string
    pending?: number
    dead?: number
  }
}

export type SparkSample = { t: number; n: number }

export type DnsRecord = {
  id: string
  label: string
  host: string
  type: string
  value: string
  ready?: boolean
}
