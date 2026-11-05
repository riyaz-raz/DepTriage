package llm

import "encoding/json"

// VerdictSchema is the shared JSON Schema contract for all LLM providers.
var VerdictSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict", "confidence", "summary", "reasons", "breaking_change_suspected", "security_relevant"],
  "properties": {
    "verdict": {"type": "string", "enum": ["safe", "review", "risk"]},
    "confidence": {"type": "number", "minimum": 0.0, "maximum": 1.0},
    "summary": {"type": "string", "minLength": 1, "maxLength": 200},
    "reasons": {"type": "array", "items": {"type": "string"}},
    "breaking_change_suspected": {"type": "boolean"},
    "security_relevant": {"type": "boolean"}
  }
}`)
