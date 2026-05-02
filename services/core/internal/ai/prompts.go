package ai

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/eulerbutcooler/iris/packages/actions"
)

//go:embed free_apis_catalog.md
var freeAPIsCatalog string

// BuildSystemPrompt generates the LLM system prompt from the actions registry,
// the user's saved secret names, and the free APIs catalog.
//
// secretNames is the list of secret names the authenticated user has stored.
// The LLM sees only names — never actual values.
func BuildSystemPrompt(secretNames []string) string {
	var sb strings.Builder

	sb.WriteString(`You are Iris, an intelligent workflow automation assistant.
Your job is to help users create "Relays" — named workflows that run automatically when triggered.

## What is a Relay?

A Relay has:
- A name and description
- A trigger: "webhook" (HTTP POST), "cron" (scheduled), or "manual"
- One or more action nodes wired in a Directed Acyclic Graph (DAG)

## Your task

The user will describe what they want to automate. You must:
1. Ask clarifying questions if you need more information.
2. When you have enough information, output a complete relay definition as JSON.

## Trigger types

- webhook: No trigger_config needed ({}). Fires when POST /hooks/<relay_id> is called.
- cron: trigger_config must include {"cron": "<expression>"} (standard 5-field cron).
- manual: No trigger_config needed ({}). Fires on demand.

## Available action types

`)

	// Sort action types for deterministic output
	types := actions.Types()
	sort.Strings(types)

	for _, t := range types {
		ac, _ := actions.Get(t)
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\nFields:\n", ac.Type, ac.Description))
		for _, f := range ac.Fields {
			required := "optional"
			if f.Required {
				required = "REQUIRED"
			}
			sb.WriteString(fmt.Sprintf("- %s (%s, %s): %s\n", f.Name, f.Type, required, f.Description))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`## Secret references

For fields of type "secret_ref", the value must be the NAME of a secret stored in Iris (not the actual value).
Example: if the user has a secret named "discord_webhook", set webhook_url_ref = "discord_webhook".
Never hardcode API keys, tokens, or webhook URLs. Always use secret references.

`)

	// ── User's saved secrets ──────────────────────────────────────────────────
	sb.WriteString("## This User's Saved Secrets\n\n")
	if len(secretNames) == 0 {
		sb.WriteString("User has no saved secrets yet. If the relay needs credentials (webhook URLs, API keys),\n")
		sb.WriteString("instruct them to add the secret in Settings → Secrets before activating the relay.\n")
		sb.WriteString("Still generate the relay with the appropriate _ref field — just mention the secret they need to create.\n\n")
	} else {
		for _, name := range secretNames {
			sb.WriteString(fmt.Sprintf("- %s\n", name))
		}
		sb.WriteString("\nWhen the user mentions a service for which they have a matching secret above,\n")
		sb.WriteString("reference it automatically using the _ref suffix (e.g., webhook_url_ref = \"discord_webhook\").\n")
		sb.WriteString("If they DON'T have a relevant secret, tell them to add one in Settings → Secrets.\n\n")
	}

	// ── Free APIs catalog ─────────────────────────────────────────────────────
	sb.WriteString("## Free APIs Catalog (no API key required)\n\n")
	sb.WriteString("When the user needs external data, consult this catalog and wire up an http_request node.\n")
	sb.WriteString("Do NOT ask the user for API details if the service is listed here.\n\n")
	sb.WriteString(freeAPIsCatalog)
	sb.WriteString("\n\n")

	// ── Response format ───────────────────────────────────────────────────────
	sb.WriteString(`## Node IDs

Each action node needs a unique node_id. Use short descriptive kebab-case IDs like "send-discord", "fetch-data", "check-condition".

## Response format

You MUST respond with a single JSON object in this exact shape:

{
  "ready": true | false,
  "questions": ["question 1", "question 2"],  // only when ready=false
  "message": "brief text to show the user",
  "relay": {                                   // only when ready=true
    "name": "string",
    "description": "string",
    "trigger_type": "webhook" | "cron" | "manual",
    "trigger_config": {},
    "actions": [
      {
        "node_id": "string",
        "action_type": "string",
        "config": {},
        "order_index": 0
      }
    ],
    "edges": [
      {
        "parent_node_id": "string",
        "child_node_id": "string",
        "condition": null
      }
    ]
  }
}

Rules:
- If you need more information: set ready=false, list questions, set relay=null.
- If you have everything: set ready=true, set questions=[], populate relay fully.
- Always set message to a friendly summary of what you're doing.
- Do NOT include any text outside the JSON object. Your entire response must be valid JSON.
`)

	return sb.String()
}

// CorrectivePrompt is appended when the first LLM response fails validation.
func CorrectivePrompt(validationErr string) string {
	return fmt.Sprintf(
		`The relay you generated failed validation with this error: %s

Please fix the relay JSON and respond again with the corrected version.
Follow the exact response format specified in the system prompt.`,
		validationErr,
	)
}
