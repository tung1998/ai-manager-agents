import { vi as uiVi, en as uiEn } from '@nuxt/ui/locale'
import vi, { type MessageKey } from '~/locales/vi'
import en from '~/locales/en'

export type Lang = 'vi' | 'en'

// One copy for the whole app: useLang is called everywhere, also outside a
// component (renderMarkdown on every render, label getters, apiError), where a
// useCookie or computed made per call is never cleaned up (a cookieStore
// listener and a watch each time: the tab's memory grew without end).
const COOKIE = 'office-lang'
let shared: { lang: Ref<Lang>, uiLocale: ComputedRef<typeof uiVi>, dateLocale: ComputedRef<string> } | undefined
function readCookie(): Lang {
  if (typeof document === 'undefined') return 'vi'
  return /(?:^|;\s*)office-lang=en(?:;|$)/.test(document.cookie) ? 'en' : 'vi'
}

export function useLang() {
  if (!shared) {
    const lang = ref<Lang>(readCookie())
    shared = {
      lang,
      uiLocale: computed(() => (lang.value === 'en' ? uiEn : uiVi)),
      dateLocale: computed(() => (lang.value === 'en' ? 'en-US' : 'vi-VN'))
    }
  }
  const { lang, uiLocale, dateLocale } = shared

  function setLang(l: Lang) {
    lang.value = l
    if (typeof document !== 'undefined') document.cookie = `${COOKIE}=${l}; path=/; max-age=31536000; samesite=lax`
  }

  function t(key: MessageKey, params?: Record<string, string | number>): string {
    const locales = lang.value === 'en' ? en : vi
    let text = locales[key] ?? vi[key] ?? key
    if (params) {
      Object.entries(params).forEach(([name, value]) => {
        text = text.replace(new RegExp(`\\{${name}\\}`, 'g'), String(value))
      })
    }
    return text
  }

  // when something happened: the hour alone today, the day too before that,
  // the year too before this year
  function when(d: string | number | Date, opts: { seconds?: boolean } = {}): string {
    const at = new Date(d)
    const now = new Date()
    const o: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit' }
    if (opts.seconds) o.second = '2-digit'
    if (at.toDateString() !== now.toDateString()) {
      o.day = '2-digit'
      o.month = '2-digit'
      if (at.getFullYear() !== now.getFullYear()) o.year = 'numeric'
    }
    return at.toLocaleString(dateLocale.value, o)
  }

  return {
    lang,
    setLang,
    t,
    when,
    uiLocale,
    dateLocale,
  }
}
