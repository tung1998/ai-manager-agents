// Copy text (a link, an id, a message) with a toast.
export function useCopy() {
  const toast = useToast()
  const { t } = useLang()
  return async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      toast.add({ title: t('common.copied'), color: 'success' })
    } catch {
      toast.add({ title: text, color: 'neutral' }) // no clipboard (http, blocked): show it to copy by hand
    }
  }
}
