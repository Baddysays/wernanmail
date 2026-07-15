import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import styles from './MailportPage.module.css'

/**
 * Mailport — embeddable mail surface for host products.
 * Drop /mailport?theme=host into an iframe; scoped API tokens come later.
 */
export function MailportPage() {
  const { t } = useTranslation()
  const params = useMemo(() => new URLSearchParams(window.location.search), [])
  const theme = params.get('theme') || 'inherit'
  const compact = params.get('compact') === '1'

  return (
    <div className={`${styles.root} ${compact ? styles.compact : ''}`} data-theme={theme}>
      <header className={styles.head}>
        <strong className={styles.brand}>Wernanmail</strong>
        <span className={styles.badge}>Mailport</span>
      </header>
      <main className={styles.body}>
        <h1>{t('mailport.title')}</h1>
        <p>{t('mailport.body')}</p>
        <ol className={styles.steps}>
          <li>{t('mailport.step1')}</li>
          <li>{t('mailport.step2')}</li>
          <li>{t('mailport.step3')}</li>
        </ol>
        <pre className={styles.code}>{`<iframe
  src="https://your-host/mailport?compact=1"
  title="Mailport"
  style="width:100%;height:480px;border:0"
></iframe>`}</pre>
      </main>
    </div>
  )
}
