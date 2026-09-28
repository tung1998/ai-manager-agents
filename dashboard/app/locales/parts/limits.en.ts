import type vi from './limits.vi'

const en: Record<keyof typeof vi, string> = {
  'limits.title': 'Usage limits · {name}',
  'limits.window.five_hour': '5-hour limit',
  'limits.window.seven_day': 'Weekly · all models',
  'limits.weeklyModel': 'Weekly · {model}',
  'limits.resetsIn': 'resets in {time}',
  'limits.resetsAt': 'resets {time}',
  'limits.resetDone': 'reset',
  'limits.context': 'Context window',
  'limits.contextTitle': 'Context used {pct}%',
  'limits.contextUnknown': 'unknown yet',
  'limits.fromLastRun': 'As of the latest run'
}
export default en
