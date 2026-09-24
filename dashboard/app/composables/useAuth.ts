export interface OfficeUser {
  id: string
  email: string
  name: string
  role: 'admin' | 'member'
  disabled: boolean
  created_at: string
  last_login_at: string | null
}

/** Error message from the Go API ({ error: "..." }) or a generic fallback. */
export function apiError(e: unknown, fallback = 'Có lỗi xảy ra, thử lại sau'): string {
  const data = (e as { data?: { error?: string } })?.data
  return data?.error ?? fallback
}

export function useAuth() {
  const user = useState<OfficeUser | null>('auth:user', () => null)
  const loaded = useState('auth:loaded', () => false)

  async function fetchMe() {
    try {
      const res = await $fetch<{ user: OfficeUser }>('/api/auth/me')
      user.value = res.user
    } catch {
      user.value = null
    } finally {
      loaded.value = true
    }
    return user.value
  }

  async function login(email: string, password: string) {
    const res = await $fetch<{ user: OfficeUser }>('/api/auth/login', {
      method: 'POST',
      body: { email, password }
    })
    user.value = res.user
    loaded.value = true
  }

  async function logout() {
    await $fetch('/api/auth/logout', { method: 'POST', body: {} }).catch(() => {})
    user.value = null
    await navigateTo('/login')
  }

  const isAdmin = computed(() => user.value?.role === 'admin')

  return { user, loaded, isAdmin, fetchMe, login, logout }
}
