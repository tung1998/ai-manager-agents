// usePhone: true on a phone-width screen (below Tailwind's sm), where some
// views change shape (e.g. a task's chat floats behind a button).
export function usePhone() {
  const phone = ref(false)
  onMounted(() => {
    const mq = window.matchMedia('(max-width: 639.98px)')
    const set = () => { phone.value = mq.matches }
    set()
    mq.addEventListener('change', set)
    onBeforeUnmount(() => mq.removeEventListener('change', set))
  })
  return phone
}
