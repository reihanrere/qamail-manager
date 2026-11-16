import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { ChevronsLeft, ChevronsRight, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { SheetClose, SheetTitle } from '@/components/ui/sheet'
import { cn } from '@/lib/utils'
import { useAppStore } from '@/store/app.store'
import { useIsAdmin } from '@/features/auth/queries'
import { navItemsFor } from './nav-items'
import { UserMenu } from './user-menu'

interface AppSidebarProps {
  className?: string
  /** Rendered inside the mobile sheet: always expanded, fills the sheet, no collapse toggle */
  mobile?: boolean
}

/**
 * Main navigation. On desktop it can collapse to icons; with `mobile` it fills the sheet
 * opened from the header and closes after navigating.
 */
export function AppSidebar({ className, mobile = false }: AppSidebarProps) {
  const { t } = useTranslation()
  const navItems = navItemsFor(useIsAdmin())
  const collapsedPreference = useAppStore((state) => state.sidebarCollapsed)
  const sidebarCollapsed = !mobile && collapsedPreference
  const toggleSidebar = useAppStore((state) => state.toggleSidebar)
  const setMobileSidebarOpen = useAppStore((state) => state.setMobileSidebarOpen)

  const handleNavClick = () => {
    setMobileSidebarOpen(false)
  }

  return (
    <aside
      className={cn(
        'flex shrink-0 flex-col bg-card',
        mobile
          ? 'h-full w-full'
          : cn('sticky top-0 h-svh border-r transition-all duration-200', sidebarCollapsed ? 'w-16' : 'w-64'),
        className,
      )}
    >
      <div className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
        <div className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary text-sm font-semibold text-primary-foreground">
          Q
        </div>
        {mobile ? (
          <>
            <SheetTitle className="min-w-0 flex-1 truncate text-sm font-semibold">QAMail Manager</SheetTitle>
            <SheetClose asChild>
              <Button
                variant="ghost"
                size="icon"
                className="-mr-2 shrink-0"
                aria-label={t('nav.closeSidebar')}
              >
                <X className="size-4" />
              </Button>
            </SheetClose>
          </>
        ) : (
          !sidebarCollapsed && <span className="truncate text-sm font-semibold">QAMail Manager</span>
        )}
      </div>

      <nav className="flex flex-1 flex-col gap-1 p-2">
        {navItems.map((item) => {
          const Icon = item.icon
          return (
            <Link
              key={item.label}
              to={item.to}
              onClick={handleNavClick}
              className={cn(
                'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground',
                sidebarCollapsed && 'justify-center px-0',
              )}
              activeProps={{ className: 'bg-accent text-accent-foreground' }}
            >
              <Icon className="size-4 shrink-0" />
              {!sidebarCollapsed && <span>{t(item.label)}</span>}
            </Link>
          )
        })}
      </nav>

      <Separator />
      <UserMenu collapsed={sidebarCollapsed} />

      {!mobile && (
        <>
          <Separator />
          <div className={cn('flex p-2', sidebarCollapsed ? 'justify-center' : 'justify-end')}>
            <Button
              variant="ghost"
              size="icon"
              onClick={toggleSidebar}
              aria-label={sidebarCollapsed ? t('nav.expandSidebar') : t('nav.collapseSidebar')}
            >
              {sidebarCollapsed ? <ChevronsRight className="size-4" /> : <ChevronsLeft className="size-4" />}
            </Button>
          </div>
        </>
      )}
    </aside>
  )
}
