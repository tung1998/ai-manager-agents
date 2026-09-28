import type vi from './limits.vi'

const en: Record<keyof typeof vi, string> = {
  'assistant.title': 'Office assistant',
  'assistant.intro': 'Ask about projects, costs, errors; have settings changed or work assigned. Every change goes through an approval card; code work is handed to the chat of the project.',
  'assistant.missing': 'The office could not set up its assistant (needs the solo template).',
  'assistant.scopeProject': 'This project',
  'assistant.scopeOffice': 'Whole office',
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
