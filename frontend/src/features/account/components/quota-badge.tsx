import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import type { AccountQuota } from '@/types/account'

/** "7/10 accounts" for a limited user, "7 accounts, no limit" for an admin. */
export function QuotaBadge({ quota }: { quota: AccountQuota | undefined }) {
  const { t } = useTranslation()
  if (!quota) return null
  if (quota.limit === null) {
    return <Badge variant="secondary">{t('quota.unlimited', { used: quota.used })}</Badge>
  }
  return (
    <Badge variant={quota.remaining === 0 ? 'destructive' : 'secondary'}>
      {t('quota.usage', { used: quota.used, limit: quota.limit })}
    </Badge>
  )
}
