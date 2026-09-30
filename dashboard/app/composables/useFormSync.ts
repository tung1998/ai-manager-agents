// syncForm fills a form from what the server sent, and again when it sends
// newer data (the live updates, ADR-072) only while the person has not
// changed the form since: what they are typing is never overwritten. After a
// save, call the returned reset so the next data is taken again.
export function syncForm<T>(source: Ref<T | null | undefined> | (() => T | null | undefined), form: object | Ref<unknown>, fill: (d: T) => void) {
  const snap = () => JSON.stringify(isRef(form) ? form.value : toRaw(form))
  let filled: string | null = null // the form as last filled: unchanged since = not edited
  const take = (d: T | null | undefined) => {
    if (d == null) return
    if (filled !== null && snap() !== filled) return // being edited: leave it
    fill(d)
    filled = snap()
  }
  watch(source, take, { immediate: true, deep: false })
  return () => { filled = null; take(typeof source === 'function' ? source() : source.value) }
}
