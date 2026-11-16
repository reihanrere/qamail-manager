import { useTranslation } from 'react-i18next'
import { Label } from '@/components/ui/label'
import { FIELD_GROUPS, toDraft, type FieldDef } from '../fields'
import type { AppSettings } from '@/types/settings'

const FIELDS = FIELD_GROUPS.flatMap((group) => group.fields)

/** Settings as plain text, for users who cannot change them. Only the values the server sent are listed. */
export function ReadOnlySettings({ settings }: { settings: AppSettings }) {
  const { t } = useTranslation()
  const shown = FIELDS.filter((field) => settings.editable[field.key] !== undefined)

  const valueText = (field: FieldDef) => {
    const value = settings.editable[field.key]
    if (field.kind === 'provider') {
      return settings.providers.find((p) => p.name === value)?.label ?? String(value)
    }
    if (field.kind === 'select') {
      return (t as (key: string) => string)(`settingsOptions.${field.key}.${String(value)}`)
    }
    return toDraft(value) || t('settingsForm.empty')
  }

  return (
    <div>
      <p className="border-b p-4 text-sm text-muted-foreground">{t('settingsForm.adminOnly')}</p>
      {shown.map((field) => (
        <div
          key={field.key}
          className="flex flex-col gap-1 p-4 sm:flex-row sm:items-center sm:justify-between sm:gap-6"
        >
          <div className="min-w-0 space-y-1">
            <Label>{t(`settingsFields.${field.key}.label`)}</Label>
            <p className="text-sm text-muted-foreground">{t(`settingsFields.${field.key}.description`)}</p>
          </div>
          <span className="font-mono text-sm break-words sm:max-w-[45%] sm:text-right">
            {valueText(field)}
          </span>
        </div>
      ))}
    </div>
  )
}
