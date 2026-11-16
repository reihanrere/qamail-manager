import { Inbox, LayoutDashboard, Settings, UserCog, Users } from 'lucide-react'

const allNavItems = [
  { label: 'nav.dashboard', to: '/dashboard', icon: LayoutDashboard, adminOnly: false },
  { label: 'nav.accounts', to: '/accounts', icon: Users, adminOnly: false },
  { label: 'nav.inbox', to: '/inbox', icon: Inbox, adminOnly: false },
  { label: 'nav.users', to: '/users', icon: UserCog, adminOnly: true },
  { label: 'nav.settings', to: '/settings', icon: Settings, adminOnly: false },
] as const

/** Sidebar entries for the current user: Users is for admins only. */
export function navItemsFor(isAdmin: boolean) {
  return allNavItems.filter((item) => isAdmin || !item.adminOnly)
}
