import type vi from './team.vi'

const en: Record<keyof typeof vi, string> = {
  'team.agentCount': '{n} agents',
  'team.noAgents': 'No agents yet',
  'team.starterPack': 'Starter pack',
  'team.default': 'Default',
  'team.defaultHint': 'Answers chats, bots and automations that name no agent',
  'team.defaultAgent': 'Default agent',
  'team.defaultAgentLower': 'default agent',
  'team.defaultSet': '{name} is now the default agent',
  'team.makeDefault': 'Make default',
  'team.open': 'Open agent page',
  'team.export': 'Download agents JSON',
  'team.applyPack': 'Use this pack',
  'team.packApplied': 'Pack applied',
  'team.replacePack': 'Replace with another pack',
  'team.replaceWarn': 'The pack\'s agents replace the current ones (agents with the same key keep their chats). The current state can still be restored from History.',
  'team.replaceBtn': 'Replace agents',
  'team.addAndConfigure': 'Add and configure',
  'team.empty.title': 'This project has no agents yet',
  'team.empty.desc': 'Pick a starter pack (agents and workflows), or let AI propose one for the project.',

  // PackPicker
  'team.pack.aiSuggest': 'AI suggestion',
  'team.pack.aiRecommended': '· recommended',
  'team.pack.aiDesc': 'Scans the project, reads CLAUDE.md, AGENTS.md, .claude/agents… then picks a pack and tunes the agents. Needs an AI connection.',
  'team.pack.workflowCount': '{n} workflows',

  // RevisionHistory
  'team.rev.intro': 'Each time agents change, office saves the state just before. Restoring is saved too, so it can always be undone.',
  'team.rev.restoreConfirm': 'Bring the project\'s agents back to this state? The current state is still kept in the history.',
  'team.rev.pack': 'applying pack {arg}',

  // setup with AI
  'team.setup.pack': 'Starter pack',
  'team.setup.hasAgentsWarn': 'This project has {n} agents. Applying the new setup replaces them (restorable from History).',

  // home: getting started
  'team.home.step3Title': 'Give projects their agents',
  'team.home.step3TextDone': '{n}/{total} projects have agents',
  'team.home.step3TextTodo': 'One agent, a dev team or a council'
}

export default en
