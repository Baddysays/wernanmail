import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  MOOD_IDS,
  resolveMood,
  updateSettings,
  useSettings,
  type MoodId,
} from '../store/settings'
import styles from './LandingPage.module.css'

const INSTALL_CMD = 'curl -fsSL https://raw.githubusercontent.com/Baddysays/wernanmail/main/install.sh | bash'
const GITHUB_URL = 'https://github.com/Baddysays/wernanmail'

function useInView<T extends HTMLElement>(margin = '-12%') {
  const ref = useRef<T | null>(null)
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const io = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          setVisible(true)
          io.disconnect()
        }
      },
      { rootMargin: margin, threshold: 0.12 },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [margin])

  return { ref, visible }
}

export function LandingPage() {
  const { t, i18n } = useTranslation()
  const settings = useSettings()
  const effectiveMood = resolveMood(settings.mood)
  const [copied, setCopied] = useState(false)
  const [solidNav, setSolidNav] = useState(false)
  const why = useInView<HTMLElement>()
  const peek = useInView<HTMLElement>()
  const moods = useInView<HTMLElement>()
  const install = useInView<HTMLElement>()

  useEffect(() => {
    try {
      if (localStorage.getItem('wernanmail.settings')) return
    } catch {
      /* ignore */
    }
    const host = window.location.hostname
    const preferRu = /\.ru$/i.test(host) || navigator.language.toLowerCase().startsWith('ru')
    if (preferRu && settings.language !== 'ru') {
      updateSettings({ language: 'ru' })
    }
  }, [settings.language])

  const heroRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    const el = heroRef.current
    if (!el) return
    const io = new IntersectionObserver(
      ([entry]) => {
        setSolidNav(!(entry?.isIntersecting ?? true))
      },
      { threshold: 0.55 },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [])

  function selectMood(id: MoodId) {
    updateSettings({ mood: id })
  }

  function toggleLang() {
    updateSettings({ language: i18n.language === 'ru' ? 'en' : 'ru' })
  }

  async function copyInstall() {
    try {
      await navigator.clipboard.writeText(INSTALL_CMD)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1800)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className={styles.page} data-landing>
      <header className={`${styles.top} ${solidNav ? styles.topSolid : ''}`}>
        <a className={styles.topBrand} href="#top" aria-label={t('app.name')}>
          <svg viewBox="0 0 32 32" width="22" height="22" aria-hidden>
            <rect width="32" height="32" rx="8" fill="currentColor" opacity="0.14" />
            <path
              d="M7 11.2c0-.66.54-1.2 1.2-1.2h15.6c.66 0 1.2.54 1.2 1.2v9.6c0 .66-.54 1.2-1.2 1.2H8.2c-.66 0-1.2-.54-1.2-1.2v-9.6zm2.1.55 6.9 4.55 6.9-4.55"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.6"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
          <span>{t('app.name')}</span>
        </a>
        <nav className={styles.topNav} aria-label={t('landing.navLabel')}>
          <a href="#why">{t('landing.nav.why')}</a>
          <a href="#moods">{t('landing.nav.moods')}</a>
          <a href="#install">{t('landing.nav.install')}</a>
          <button type="button" className={styles.langBtn} onClick={toggleLang}>
            {i18n.language === 'ru' ? 'EN' : 'RU'}
          </button>
          <Link className={styles.topLogin} to="/login">
            {t('landing.cta.open')}
          </Link>
        </nav>
      </header>

      <section ref={heroRef} className={styles.hero} id="top" aria-labelledby="landing-brand">
        <div className={styles.aurora} aria-hidden>
          <span className={styles.blobA} />
          <span className={styles.blobB} />
          <span className={styles.blobC} />
        </div>
        <div className={styles.heroGrain} aria-hidden />
        <div className={styles.mailStream} aria-hidden>
          <svg className={styles.streamPath} viewBox="0 0 1200 680" preserveAspectRatio="xMidYMid slice">
            <path
              className={styles.streamLine}
              d="M-40 420 C 180 280, 320 500, 520 360 S 820 220, 1040 340 S 1240 480, 1320 300"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.25"
              strokeLinecap="round"
            />
          </svg>
          <span className={styles.envelope} />
          <span className={`${styles.envelope} ${styles.envelopeB}`} />
          <span className={`${styles.envelope} ${styles.envelopeC}`} />
        </div>

        <div className={styles.heroCopy}>
          <h1 id="landing-brand" className={styles.brand}>
            {t('app.name')}
          </h1>
          <p className={styles.headline}>{t('landing.headline')}</p>
          <p className={styles.lede}>{t('landing.lede')}</p>
          <div className={styles.ctaRow}>
            <Link className={styles.ctaPrimary} to="/login">
              {t('landing.cta.open')}
            </Link>
            <a className={styles.ctaGhost} href="#install">
              {t('landing.cta.install')}
            </a>
          </div>
        </div>
      </section>

      <section
        id="why"
        ref={why.ref}
        className={`${styles.section} ${styles.why} ${why.visible ? styles.in : ''}`}
        aria-labelledby="why-title"
      >
        <p className={styles.kicker}>{t('landing.why.kicker')}</p>
        <h2 id="why-title" className={styles.sectionTitle}>
          {t('landing.why.title')}
        </h2>
        <p className={styles.sectionBody}>{t('landing.why.body')}</p>
        <div className={styles.breath} aria-hidden>
          <div className={styles.breathTrack}>
            <span className={styles.breathFill} />
          </div>
          <p className={styles.breathLabel}>{t('landing.why.meter')}</p>
        </div>
      </section>

      <section
        ref={peek.ref}
        className={`${styles.peek} ${peek.visible ? styles.in : ''}`}
        aria-label={t('landing.peek.label')}
      >
        <div className={styles.peekPlane}>
          <div className={styles.peekChrome}>
            <span />
            <span />
            <span />
          </div>
          <div className={styles.peekBody}>
            <aside className={styles.peekSide}>
              <strong>{t('landing.peek.inbox')}</strong>
              <em>{t('landing.peek.sent')}</em>
              <em>{t('landing.peek.drafts')}</em>
            </aside>
            <div className={styles.peekList}>
              {[0, 1, 2, 3].map((i) => (
                <div key={i} className={styles.peekRow} style={{ animationDelay: `${120 + i * 70}ms` }}>
                  <b />
                  <i />
                  <s />
                </div>
              ))}
            </div>
            <div className={styles.peekRead}>
              <h3>{t('landing.peek.subject')}</h3>
              <p>{t('landing.peek.message')}</p>
            </div>
          </div>
        </div>
      </section>

      <section
        id="moods"
        ref={moods.ref}
        className={`${styles.section} ${styles.moodsSection} ${moods.visible ? styles.in : ''}`}
        aria-labelledby="moods-title"
      >
        <p className={styles.kicker}>{t('landing.moods.kicker')}</p>
        <h2 id="moods-title" className={styles.sectionTitle}>
          {t('landing.moods.title')}
        </h2>
        <p className={styles.sectionBody}>{t('landing.moods.body')}</p>
        <div className={styles.moodPick} role="radiogroup" aria-label={t('settings.mood')}>
          {MOOD_IDS.map((id) => (
            <button
              key={id}
              type="button"
              role="radio"
              aria-checked={effectiveMood === id}
              className={`${styles.moodBtn} ${effectiveMood === id ? styles.moodBtnActive : ''}`}
              data-mood-swatch={id}
              onClick={() => selectMood(id)}
            >
              <span className={styles.moodSwatch} />
              <span className={styles.moodName}>{t(`settings.moods.${id}`)}</span>
            </button>
          ))}
        </div>
      </section>

      <section
        id="install"
        ref={install.ref}
        className={`${styles.section} ${styles.install} ${install.visible ? styles.in : ''}`}
        aria-labelledby="install-title"
      >
        <p className={styles.kicker}>{t('landing.install.kicker')}</p>
        <h2 id="install-title" className={styles.sectionTitle}>
          {t('landing.install.title')}
        </h2>
        <p className={styles.sectionBody}>{t('landing.install.body')}</p>
        <div className={styles.terminal}>
          <code>{INSTALL_CMD}</code>
          <button type="button" className={styles.copyBtn} onClick={() => void copyInstall()}>
            {copied ? t('landing.install.copied') : t('landing.install.copy')}
          </button>
        </div>
        <div className={styles.installLinks}>
          <Link className={styles.ctaPrimary} to="/login">
            {t('landing.cta.open')}
          </Link>
          <a className={styles.ctaGhost} href={GITHUB_URL} target="_blank" rel="noreferrer">
            {t('landing.cta.github')}
          </a>
          <a className={styles.ctaGhost} href="/admin/">
            {t('landing.cta.admin')}
          </a>
        </div>
      </section>

      <footer className={styles.footer}>
        <p>
          <strong>{t('app.name')}</strong>
          <span> — {t('landing.footer.line')}</span>
        </p>
        <p className={styles.footerHost}>wernanmail.ru</p>
      </footer>
    </div>
  )
}
