{
  "description": "string",
  "reason": "string",
  "confidence": 0.0,
  "default_agent": "string?",
  "agents": [
    {"id": "string", "key": "string?", "name": "string?", "role": "string?", "description": "string?",
     "model_tier": "strong|balanced|fast?", "instructions": "string?",
     "level": "read|propose|check|edit|operate?", "caps": ["string"]?, "commands": ["string"]?,
     "reason": "string"}
  ],
  "workflows": [{"name": "string", "reason": "string"}],
  "automations": [{"name": "string", "reason": "string"}],
  "skills": [{"name": "string", "reason": "string"}],
  "mcp": [{"name": "string", "reason": "string"}],
  "quick_checks": "string?",
  "notes": ["string"]
}
