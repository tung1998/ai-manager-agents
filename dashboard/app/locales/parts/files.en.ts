import type vi from './files.vi'

const en: Record<keyof typeof vi, string> = {
  'files.section': 'Files',
  'files.showHidden': 'Show hidden files',
  'files.hiddenInfo': 'Files in .gitignore and secret files (.env, keys…) are hidden by default',
  'files.hiddenCount': '{n} hidden',
  'files.more': '… and {n} more',
  'files.empty': 'Empty folder',
  'files.pick': 'Pick a file to view or edit',
  'files.newFile': 'New file',
  'files.newPrompt': 'New file path (from the project folder)',
  'files.refresh': 'Reload',
  'files.save': 'Save',
  'files.revert': 'Discard changes',
  'files.saved': 'Saved {path}',
  'files.unsaved': 'Unsaved',
  'files.changes': 'Changes since the last commit',
  'files.noChanges': 'No changes since the last commit (or the file is not tracked by git)',
  'files.binary': 'Binary file, not editable on the dashboard',
  'files.tooLarge': 'File too large ({size}) to open on the dashboard',
  'files.ignored': 'gitignore',
  'files.secret': 'secret',
  'files.protected': 'protected',
  'files.confirmSensitive': '{path} is a secret or protected file. Save anyway?',
  'files.conflict': 'The file changed after you opened it',
  'files.reload': 'Load the new version',
  'files.leave': 'You have unsaved changes. Discard them?'
}

export default en
