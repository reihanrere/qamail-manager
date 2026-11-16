import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { Ban, CircleCheck, KeyRound, Loader2, Plus, RefreshCw, Trash2, UserCog } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { EmptyState } from '@/components/common/empty-state'
import { CreatedAt } from '@/features/account/components/account-row'
import { QuotaBadge } from '@/features/account/components/quota-badge'
import { useMe } from '@/features/auth/queries'
import { apiErrorMessage } from '@/lib/api-error'
import { useRefresh } from '@/hooks/use-refresh'
import { cn } from '@/lib/utils'
import { userApi } from '../api'
import { userKeys, useUsers } from '../queries'
import { userActionBlock, type UserActionBlock } from '../rules'
import { CreateUserDialog, ResetPasswordDialog } from './user-dialogs'
import type { ManagedUser } from '@/types/user'

interface RowProps {
  user: ManagedUser
  block: UserActionBlock | null
  isUpdating: boolean
  isSelf: boolean
  onToggleDisabled: () => void
  onResetPassword: () => void
  onDelete: () => void
}

/** Users page body: header, then a table (desktop) or list (mobile) with per-user actions. */
export function UsersList() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const me = useMe().data
  const { data: users, isPending, error, refetch } = useUsers()
  const [resetting, setResetting] = useState<ManagedUser | null>(null)
  const [pendingDelete, setPendingDelete] = useState<ManagedUser | null>(null)
  const { isRefreshing, refresh } = useRefresh(() => refetch())

  const toggleDisabled = useMutation({
    mutationFn: (user: ManagedUser) => userApi.update(user.id, { disabled: !user.disabled }),
    onSuccess: (_, user) => {
      toast.success(t(user.disabled ? 'users.enabled' : 'users.disabled', { email: user.email }))
      queryClient.invalidateQueries({ queryKey: userKeys.all })
    },
    onError: (error) => toast.error(apiErrorMessage(error, t('users.updateFailed'))),
  })

  const deleteUser = useMutation({
    mutationFn: (user: ManagedUser) => userApi.delete(user.id, user.accounts > 0),
    onSuccess: (_, user) => {
      toast.success(t('users.deleted', { email: user.email }))
      setPendingDelete(null)
      queryClient.invalidateQueries({ queryKey: userKeys.all })
    },
    onError: (error) => {
      toast.error(apiErrorMessage(error, t('users.deleteFailed')))
      // The account count may have changed since the list was loaded
      queryClient.invalidateQueries({ queryKey: userKeys.all })
      setPendingDelete(null)
    },
  })

  const list = users ?? []
  const rowProps = (user: ManagedUser): RowProps => ({
    user,
    block: userActionBlock(user, me?.id, list),
    isSelf: user.id === me?.id,
    isUpdating: toggleDisabled.isPending && toggleDisabled.variables?.id === user.id,
    onToggleDisabled: () => toggleDisabled.mutate(user),
    onResetPassword: () => setResetting(user),
    onDelete: () => setPendingDelete(user),
  })

  return (
    <TooltipProvider delayDuration={200}>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <h2 className="text-2xl font-semibold tracking-tight">{t('pages.users.title')}</h2>
          <p className="text-muted-foreground">
            {isPending ? t('users.loading') : t('users.count', { count: list.length })}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="icon"
            onClick={refresh}
            disabled={isRefreshing}
            aria-label={t('users.refresh')}
          >
            <RefreshCw className={cn('size-4', isRefreshing && 'animate-spin')} />
          </Button>
          <CreateUserDialog>
            <Button className="gap-2">
              <Plus className="size-4" />
              {t('users.addUser')}
            </Button>
          </CreateUserDialog>
        </div>
      </div>

      <Card className="gap-0 overflow-hidden py-0 shadow-sm">
        {isPending ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-12 w-full" />
            ))}
          </div>
        ) : error && !users ? (
          <div className="flex flex-col items-center gap-3 p-10 text-center">
            <p className="text-sm text-destructive">{t('users.loadFailed')}</p>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              {t('common.retry')}
            </Button>
          </div>
        ) : list.length === 0 ? (
          <EmptyState
            icon={UserCog}
            title={t('users.none')}
            description={t('users.noneHint')}
            className="m-4"
          />
        ) : (
          <>
            {/* Desktop: table */}
            <Table className="hidden md:table">
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="h-10">{t('users.columns.email')}</TableHead>
                  <TableHead className="h-10 w-28">{t('users.columns.role')}</TableHead>
                  <TableHead className="h-10 w-28">{t('users.columns.status')}</TableHead>
                  <TableHead className="h-10 w-36">{t('users.columns.accounts')}</TableHead>
                  <TableHead className="hidden h-10 w-40 lg:table-cell">
                    {t('users.columns.created')}
                  </TableHead>
                  <TableHead className="h-10 w-36 text-right">
                    <span className="sr-only">{t('users.columns.actions')}</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.map((user) => {
                  const props = rowProps(user)
                  return (
                    <TableRow key={user.id}>
                      <TableCell className="py-2.5">
                        <UserEmail user={user} isSelf={props.isSelf} />
                      </TableCell>
                      <TableCell className="py-2.5">
                        <RoleBadge role={user.role} />
                      </TableCell>
                      <TableCell className="py-2.5">
                        <StatusBadge disabled={user.disabled} />
                      </TableCell>
                      <TableCell className="py-2.5">
                        <QuotaBadge quota={user.quota} />
                      </TableCell>
                      <TableCell className="hidden py-2.5 lg:table-cell">
                        <CreatedAt date={user.createdAt} />
                      </TableCell>
                      <TableCell className="py-2.5">
                        <UserActions {...props} />
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>

            {/* Mobile: stacked list */}
            <ul className="divide-y md:hidden">
              {list.map((user) => {
                const props = rowProps(user)
                return (
                  <li key={user.id} className="flex flex-col gap-2 p-4">
                    <UserEmail user={user} isSelf={props.isSelf} />
                    <div className="flex flex-wrap items-center gap-2">
                      <RoleBadge role={user.role} />
                      <StatusBadge disabled={user.disabled} />
                      <QuotaBadge quota={user.quota} />
                    </div>
                    <div className="flex items-center justify-between gap-2">
                      <CreatedAt date={user.createdAt} inline />
                      <UserActions {...props} />
                    </div>
                  </li>
                )
              })}
            </ul>
          </>
        )}
      </Card>

      <ResetPasswordDialog user={resetting} onOpenChange={(open) => !open && setResetting(null)} />

      <AlertDialog
        open={!!pendingDelete}
        onOpenChange={(open) => !open && !deleteUser.isPending && setPendingDelete(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('users.deleteTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              <span className="font-mono break-all text-foreground">{pendingDelete?.email}</span>
              <span className="mt-2 block">
                {pendingDelete && pendingDelete.accounts > 0
                  ? t('users.deleteWithAccounts', { count: pendingDelete.accounts })
                  : t('users.deleteNoAccounts')}
              </span>
              <span className="mt-1 block font-medium text-foreground">{t('users.cannotUndo')}</span>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteUser.isPending}>{t('common.cancel')}</AlertDialogCancel>
            <Button
              variant="destructive"
              className="gap-2"
              disabled={deleteUser.isPending}
              onClick={() => pendingDelete && deleteUser.mutate(pendingDelete)}
            >
              {deleteUser.isPending && <Loader2 className="size-4 animate-spin" />}
              {t('common.delete')}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </TooltipProvider>
  )
}

function UserEmail({ user, isSelf }: { user: ManagedUser; isSelf: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className={cn('truncate font-mono text-sm', user.disabled && 'text-muted-foreground')}>
        {user.email}
      </span>
      {isSelf && <Badge variant="outline">{t('users.you')}</Badge>}
    </div>
  )
}

function RoleBadge({ role }: { role: ManagedUser['role'] }) {
  const { t } = useTranslation()
  return <Badge variant={role === 'admin' ? 'default' : 'secondary'}>{t(`users.role.${role}`)}</Badge>
}

function StatusBadge({ disabled }: { disabled: boolean }) {
  const { t } = useTranslation()
  return (
    <Badge variant={disabled ? 'destructive' : 'outline'}>
      {disabled ? t('users.status.disabled') : t('users.status.active')}
    </Badge>
  )
}

/** Reset password, disable/enable and delete for one user; the last two say why they are off. */
function UserActions({ user, block, isUpdating, onToggleDisabled, onResetPassword, onDelete }: RowProps) {
  const { t } = useTranslation()
  const blockReason = block ? t(`users.blocked.${block}`) : undefined
  // Enabling is always safe; only disabling (and deleting) is blocked
  const toggleBlocked = !user.disabled && !!block
  const toggleLabel = user.disabled ? t('users.enable') : t('users.disable')

  return (
    <div className="flex items-center justify-end gap-1">
      <IconAction label={t('users.resetPassword')} onClick={onResetPassword}>
        <KeyRound className="size-4" />
      </IconAction>
      <IconAction
        label={toggleBlocked ? blockReason! : toggleLabel}
        ariaLabel={toggleLabel}
        onClick={onToggleDisabled}
        disabled={toggleBlocked || isUpdating}
      >
        {isUpdating ? (
          <Loader2 className="size-4 animate-spin" />
        ) : user.disabled ? (
          <CircleCheck className="size-4" />
        ) : (
          <Ban className="size-4" />
        )}
      </IconAction>
      <IconAction
        label={block ? blockReason! : t('users.delete')}
        ariaLabel={t('users.delete')}
        onClick={onDelete}
        disabled={!!block}
        destructive
      >
        <Trash2 className="size-4" />
      </IconAction>
    </div>
  )
}

interface IconActionProps {
  /** Tooltip text; for a disabled action this is the reason */
  label: string
  /** Accessible name when it differs from the tooltip (disabled actions) */
  ariaLabel?: string
  onClick: () => void
  disabled?: boolean
  destructive?: boolean
  children: React.ReactNode
}

function IconAction({ label, ariaLabel, onClick, disabled, destructive, children }: IconActionProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {/* A disabled button swallows pointer events, so the span carries the tooltip */}
        <span tabIndex={disabled ? 0 : -1}>
          <Button
            variant="ghost"
            size="icon"
            className={cn(
              'size-8',
              destructive && 'text-destructive hover:bg-destructive/10 hover:text-destructive',
            )}
            onClick={onClick}
            disabled={disabled}
            aria-label={ariaLabel ?? label}
          >
            {children}
          </Button>
        </span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
