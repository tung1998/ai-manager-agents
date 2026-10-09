// useLeaveGuard asks before what is typed is lost: leaving for another page
// (or another tab of the same page) while dirty, and closing or reloading the
// browser tab. A query change that keeps the page and its tab (?c=, ?task=)
// is not a leave.
export function useLeaveGuard(dirty: () => boolean) {
  const { t } = useLang()
  const router = useRouter()
  const off = router.beforeEach((to, from) => {
    if (to.path === from.path && to.query.tab === from.query.tab) return
    if (dirty() && !confirm(t('common.leaveUnsaved'))) return false
  })
  const onUnload = (e: BeforeUnloadEvent) => {
    if (!dirty()) return
    e.preventDefault()
    e.returnValue = '' // older browsers need it to ask
  }
  onMounted(() => window.addEventListener('beforeunload', onUnload))
  onBeforeUnmount(() => {
    off()
    window.removeEventListener('beforeunload', onUnload)
  })
}
