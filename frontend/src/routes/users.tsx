import { Navigate, createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AppHeader } from '@/components/layout/app-header'
import { PageContainer } from '@/components/common/page-container'
import { useMe } from '@/features/auth/queries'
import { usersPageAccess } from '@/features/users/rules'
import { UsersList } from '@/features/users/components/users-list'

export const Route = createFileRoute('/users')({
  component: UsersPage,
})

/** Admin only: a plain user who opens /users is sent to the dashboard (the API refuses them too). */
function UsersPage() {
  const { t } = useTranslation()
  const access = usersPageAccess(useMe().data)
  if (access === 'none') return null
  if (access === 'redirect') return <Navigate to="/dashboard" replace />
  return (
    <>
      <AppHeader title={t('pages.users.title')} description={t('pages.users.description')} />
      <PageContainer>
        <UsersList />
      </PageContainer>
    </>
  )
}
