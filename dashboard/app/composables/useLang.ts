import { vi as uiVi, en as uiEn } from '@nuxt/ui/locale'
import vi, { type MessageKey } from '~/locales/vi'
import en from '~/locales/en'

export type Lang = 'vi' | 'en'

export function useLang() {
  const cookie = useCookie<Lang>('office-lang', { maxAge: 31536000 })
  const lang = useState<Lang>('office-lang', () =>
    cookie.value === 'en' ? 'en' : 'vi'
  )

  function setLang(l: Lang) {
    lang.value = l
    cookie.value = l
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

  const uiLocale = computed(() => (lang.value === 'en' ? uiEn : uiVi))
  const dateLocale = computed(() => (lang.value === 'en' ? 'en-US' : 'vi-VN'))

  return {
    lang,
    setLang,
    t,
    uiLocale,
    dateLocale,
  }
}
