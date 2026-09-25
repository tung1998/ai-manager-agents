import DOMPurify from 'dompurify'
import { marked } from 'marked'

marked.setOptions({ gfm: true, breaks: true })

/**
 * Render agent Markdown to safe HTML. Diff blocks are replaced by a note
 * because proposed changes are shown as their own cards with approve buttons.
 */
export function renderMarkdown(text: string, hideDiffs = true): string {
  let src = text
  if (hideDiffs) {
    src = src.replace(/```(?:diff|patch)[ \t]*\n[\s\S]*?```/g, '_(đề xuất thay đổi ở bên dưới)_')
  }
  return DOMPurify.sanitize(marked.parse(src, { async: false }) as string)
}
