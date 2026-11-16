import { useState, type ReactNode } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { apiErrorMessage } from '@/lib/api-error'
import { userApi } from '../api'
import { userKeys } from '../queries'
import type { UserRole } from '@/types/auth'
import type { ManagedUser } from '@/types/user'

interface CreateFormValues {
  email: string
  password: string
  role: UserRole
}

/** Opens a dialog to add a user with an email, a password and a role. */
export function CreateUserDialog({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const {
    register,
    handleSubmit,
    reset,
    control,
    formState: { errors },
  } = useForm<CreateFormValues>({ defaultValues: { email: '', password: '', role: 'user' } })

  const create = useMutation({
    mutationFn: (values: CreateFormValues) => userApi.create({ ...values, email: values.email.trim() }),
    onSuccess: (user) => {
      toast.success(t('users.created', { email: user.email }))
      queryClient.invalidateQueries({ queryKey: userKeys.all })
      setOpen(false)
      reset()
    },
    onError: (error) => toast.error(apiErrorMessage(error, t('users.createFailed'))),
  })

  const handleOpenChange = (next: boolean) => {
    if (create.isPending) return
    setOpen(next)
    if (!next) {
      reset()
      create.reset()
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>{children}</DialogTrigger>
      <DialogContent className="sm:max-w-md" showCloseButton={!create.isPending}>
        <form onSubmit={handleSubmit((values) => create.mutate(values))} className="grid gap-5" noValidate>
          <DialogHeader>
            <DialogTitle>{t('users.addTitle')}</DialogTitle>
            <DialogDescription>{t('users.addDescription')}</DialogDescription>
          </DialogHeader>

          <div className="grid gap-2">
            <Label htmlFor="new-user-email">{t('auth.email')}</Label>
            <Input
              id="new-user-email"
              type="email"
              autoComplete="off"
              disabled={create.isPending}
              aria-invalid={!!errors.email}
              {...register('email', { required: t('users.emailRequired') })}
            />
            {errors.email && <p className="text-xs text-destructive">{errors.email.message}</p>}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="new-user-password">{t('auth.password')}</Label>
            <Input
              id="new-user-password"
              type="password"
              autoComplete="new-password"
              disabled={create.isPending}
              aria-invalid={!!errors.password}
              {...register('password', { required: t('users.passwordRequired') })}
            />
            {errors.password && <p className="text-xs text-destructive">{errors.password.message}</p>}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="new-user-role">{t('users.columns.role')}</Label>
            <Controller
              control={control}
              name="role"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange} disabled={create.isPending}>
                  <SelectTrigger id="new-user-role" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="user">{t('users.role.user')}</SelectItem>
                    <SelectItem value="admin">{t('users.role.admin')}</SelectItem>
                  </SelectContent>
                </Select>
              )}
            />
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => handleOpenChange(false)}
              disabled={create.isPending}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={create.isPending} className="gap-2">
              {create.isPending && <Loader2 className="size-4 animate-spin" />}
              {create.isPending ? t('users.adding') : t('users.add')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** Controlled dialog to set a new password for a user; open while `user` is set. */
export function ResetPasswordDialog({
  user,
  onOpenChange,
}: {
  user: ManagedUser | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<{ password: string }>({ defaultValues: { password: '' } })

  const resetPassword = useMutation({
    mutationFn: (values: { password: string }) => userApi.update(user!.id, { password: values.password }),
    onSuccess: () => {
      toast.success(t('users.passwordReset', { email: user?.email ?? '' }))
      queryClient.invalidateQueries({ queryKey: userKeys.all })
      reset()
      onOpenChange(false)
    },
    onError: (error) => toast.error(apiErrorMessage(error, t('users.passwordResetFailed'))),
  })

  const handleOpenChange = (next: boolean) => {
    if (resetPassword.isPending) return
    if (!next) {
      reset()
      resetPassword.reset()
    }
    onOpenChange(next)
  }

  return (
    <Dialog open={!!user} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-md" showCloseButton={!resetPassword.isPending}>
        <form
          onSubmit={handleSubmit((values) => resetPassword.mutate(values))}
          className="grid gap-5"
          noValidate
        >
          <DialogHeader>
            <DialogTitle>{t('users.resetTitle')}</DialogTitle>
            <DialogDescription>
              <span className="font-mono break-all text-foreground">{user?.email}</span>
              <span className="mt-1 block">{t('users.resetDescription')}</span>
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-2">
            <Label htmlFor="reset-user-password">{t('users.newPassword')}</Label>
            <Input
              id="reset-user-password"
              type="password"
              autoComplete="new-password"
              autoFocus
              disabled={resetPassword.isPending}
              aria-invalid={!!errors.password}
              {...register('password', { required: t('users.passwordRequired') })}
            />
            {errors.password && <p className="text-xs text-destructive">{errors.password.message}</p>}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => handleOpenChange(false)}
              disabled={resetPassword.isPending}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={resetPassword.isPending} className="gap-2">
              {resetPassword.isPending && <Loader2 className="size-4 animate-spin" />}
              {t('users.resetPassword')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
