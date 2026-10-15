import type vi from './team.vi'

const en: Record<keyof typeof vi, string> = {
  'team.agentCount': '{n} agents',
  'team.noAgents': 'No agents yet',
  'team.starterPack': 'Starter pack',
  'team.default': 'Default',
  'team.defaultHint': 'This agent answers when a chat, a bot message (Discord/Telegram) or an automation does not name a specific agent. Use this when a project has several agents but should still have one that answers in general cases. Making another agent default retires this one from that role; its past chats stay as they are.',
  'team.defaultAgent': 'Default agent',
  'team.defaultAgentLower': 'default agent',
  'team.defaultSet': '{name} is now the default agent',
  'team.makeDefault': 'Make default',
  'team.open': 'Open agent page',
  'team.export': 'Download agents JSON',
  'team.applyPack': 'Use this pack',
  'team.packApplied': 'Pack applied',
  'team.replacePack': 'Replace with another pack',
  'team.replaceWarn': 'Applying another pack replaces every current agent of this project with the ones in the new pack. Use this when switching to a whole different set of agents instead of editing them one by one. Agents sharing an identifier with the new pack are updated in place and keep their past chats; agents with no match are removed, though the state before the swap still stays in History to view or restore.',
  'team.replaceBtn': 'Replace agents',
  'team.addAndConfigure': 'Add and configure',
  'team.empty.title': 'This project has no agents yet',
  'team.empty.desc': 'Pick a starter pack (agents and workflows), or let AI propose one for the project.',

  // AgentLibrary (ADR-153)
  'team.lib.tab': 'From the library',
  'team.lib.blankTab': 'From scratch',
  'team.lib.addOne': 'Add one agent from the library',
  'team.lib.all': 'All',
  'team.lib.pageTitle': 'Agent library',
  'team.lib.pageDesc': 'Agent templates to add to a project, or to read as a model when writing your own instructions. Click one to see it in full.',
  'team.lib.pickProject': 'Pick a project',
  'team.lib.toProject': 'Add to project',
  'team.lib.featured': 'Featured',
  'team.lib.cat.design': 'Design',
  'team.lib.count': '{n} agents',
  'team.lib.words': '{n} words',
  'team.lib.agencyNote': 'Original English text, fairly long: trim it after adding to save tokens',
  'team.lib.cat.code': 'Code',
  'team.lib.cat.plan': 'Planning',
  'team.lib.cat.customer': 'Customers',
  'team.lib.cat.content': 'Content',
  'team.lib.search': 'Search agents…',
  'team.lib.none': 'No agent matches',
  'team.lib.source': 'Original',
  'team.lib.use': 'Use this one',
  'team.lib.from': 'Based on {name}: with its description, instructions and permissions',
  'team.lib.clear': 'Drop the template',

  // PackPicker
  'team.pack.aiSuggest': 'AI suggestion',
  'team.pack.aiRecommended': '· recommended',
  'team.pack.aiDesc': 'Uses AI to pick a suitable starter pack for this project instead of you choosing by hand. The agent scans the project\'s structure, reads existing guide files such as CLAUDE.md, AGENTS.md, .claude/agents…, then picks a fitting pack and adjusts each agent\'s name, instructions and permissions to match the project. Needs an AI connection that has already been tested working.',
  'team.pack.workflowCount': '{n} workflows',

  // RevisionHistory
  'team.rev.intro': 'Each time agents change, office saves the state just before. Restoring is saved too, so it can always be undone.',
  'team.rev.restoreConfirm': 'Bring the project\'s agents back to this state? The current state is still kept in the history.',
  'team.rev.pack': 'applying pack {arg}',

  // setup with AI
  'team.setup.pack': 'Starter pack',
  'team.setup.hasAgentsWarn': 'This project has {n} agents. Applying only adds the missing ones; existing agents stay as they are.',

  // home: getting started
  'team.home.step3Title': 'Give projects their agents',
  'team.home.step3TextDone': '{n}/{total} projects have agents',
  'team.home.step3TextTodo': 'One agent, a dev team or a council'
}

export default en
