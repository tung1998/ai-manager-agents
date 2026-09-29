// Telegram / Discord bots of a project (ADR-048): the connection only. What a
// message does is an automation whose source is the channel (ADR-049).
export interface Channel {
  id: string, kind: 'telegram' | 'discord', name: string, has_token: boolean, enabled: boolean,
  allow: string[], refusal: string, approvers: string[], approval: 'ask' | 'direct', bot_name: string, last_error: string, last_message_at: string | null
}
