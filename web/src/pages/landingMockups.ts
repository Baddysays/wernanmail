/** Product shots — same files as docs/mockups/ in the GitHub repo. */
export const GITHUB_MOCKUPS_BASE =
  'https://raw.githubusercontent.com/Baddysays/wernanmail/main/docs/mockups'

export type ShowcaseId = 'inbox' | 'signin' | 'compose'

export const SHOWCASE_SHOTS: Record<
  ShowcaseId,
  { file: string; github: string; local: string }
> = {
  inbox: {
    file: 'admin-overview.png',
    github: `${GITHUB_MOCKUPS_BASE}/admin-overview.png`,
    local: '/mockups/admin-overview.png',
  },
  signin: {
    file: 'login-desktop.png',
    github: `${GITHUB_MOCKUPS_BASE}/login-desktop.png`,
    local: '/mockups/login-desktop.png',
  },
  compose: {
    file: 'compose.png',
    github: `${GITHUB_MOCKUPS_BASE}/compose.png`,
    local: '/mockups/compose.png',
  },
}

export const MOOD_COMPOSE: Partial<Record<string, string>> = {
  grove: '/mockups/compose-grove.png',
  reef: '/mockups/compose.png',
}

export const MOBILE_PAIR = {
  login: '/mockups/login-mobile.png',
  moods: '/mockups/settings-moods-mobile.png',
}

/** Prefer bundled assets; GitHub raw is fallback once CDN picks up main. */
export function mockupSrc(shot: keyof typeof SHOWCASE_SHOTS | 'moodCompose' | 'mobileLogin' | 'mobileMoods', mood?: string) {
  if (shot === 'moodCompose') {
    return MOOD_COMPOSE[mood ?? ''] ?? '/mockups/compose.png'
  }
  if (shot === 'mobileLogin') return MOBILE_PAIR.login
  if (shot === 'mobileMoods') return MOBILE_PAIR.moods
  return SHOWCASE_SHOTS[shot].local
}

export function mockupFallback(shot: ShowcaseId) {
  return SHOWCASE_SHOTS[shot].github
}
