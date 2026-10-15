// What waits for a person's approval (a proposal or a diff), decided from
// anywhere: the header's list, a toast, a browser notification (public/sw.js).
export interface Incident { kind: string, severity: 'error' | 'warning' | 'info', project_id: string, project_name: string, title: string, detail: string, at: string, link: string, id: string, key: string, prompt?: string }

export const isApproval = (x: Incident) => x.kind === 'approval' || x.kind === 'patch'

// approve or reject (skip: rejected, its agent is not run again about it)
export async function decideApproval(x: Pick<Incident, 'kind' | 'id'>, what: 'approve' | 'reject') {
  const base = x.kind === 'patch' ? '/api/patches/' : '/api/actions/'
  await $fetch(`${base}${x.id}/${what}`, { method: 'POST', body: what === 'reject' ? { skip: true } : {} })
}

// the browser's permission for notifications, asked once from the header
export function useBrowserNotify() {
  const supported = import.meta.client && 'Notification' in window && 'serviceWorker' in navigator
  const permission = useState<NotificationPermission>('notify-permission', () => supported ? Notification.permission : 'denied')
  async function enable() {
    if (!supported) return
    permission.value = await Notification.requestPermission()
  }
  return { supported, permission, enable }
}
