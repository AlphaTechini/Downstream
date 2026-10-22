package journey

import (
	"encoding/json"

	"downstream/engine/model"
)

// Schema returns the JSON Schema (draft 2020-12) for journeys.json as indented
// JSON. The flow prints it for the model; Validate remains the authority for
// rules JSON Schema cannot express (route matching, placeholder scoping).
func Schema() string {
	methods := make([]any, len(methodList))
	for i, m := range methodList {
		methods[i] = m
	}
	text := func(desc string) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": maxText, "description": desc}
	}
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"title":                "Downstream journeys.json",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"version", "journeys"},
		"properties": map[string]any{
			"version": map[string]any{"const": model.FormatVersion},
			"journeys": map[string]any{
				"type": "array", "minItems": 1, "maxItems": maxJourneys,
				"items": map[string]any{"$ref": "#/$defs/journey"},
			},
		},
		"$defs": map[string]any{
			"journey": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"id", "goal", "why_included", "steps"},
				"properties": map[string]any{
					"id":           map[string]any{"type": "string", "pattern": "^[a-z0-9_]{1,40}$", "description": "Unique within the file."},
					"goal":         text("What this user is trying to do, in plain English."),
					"why_included": text("Why this journey matters for the change under review."),
					"steps": map[string]any{
						"type": "array", "minItems": 1, "maxItems": maxSteps,
						"items": map[string]any{"$ref": "#/$defs/step"},
					},
				},
			},
			"step": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"name", "method", "path", "expect_status"},
				"properties": map[string]any{
					"name":   map[string]any{"type": "string", "pattern": "^[a-z0-9_]{1,40}$", "description": "Unique within the journey."},
					"method": map[string]any{"enum": methods},
					"path": map[string]any{
						"type": "string", "pattern": "^/", "maxLength": maxPath,
						"description": "Must match a route in the head route table (method and path pattern). May carry a query string and {{var}} placeholders. No scheme, host, or '..'.",
					},
					"headers": map[string]any{
						"type":                 "object",
						"propertyNames":        map[string]any{"pattern": "^([Aa]uthorization|[Cc]ontent-[Tt]ype|[Aa]ccept|[Xx]-[A-Za-z0-9-]+)$"},
						"additionalProperties": map[string]any{"type": "string", "maxLength": maxHeader},
						"description":          "Only Authorization, Content-Type, Accept and X-* headers. Values may carry {{var}} placeholders.",
					},
					"body": map[string]any{
						"type":        []string{"object", "array"},
						"description": "JSON request body, at most 4096 bytes when serialised. Any string inside may carry {{var}} placeholders.",
					},
					"expect_status": map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
					"extract": map[string]any{
						"type":                 "object",
						"maxProperties":        maxExtracts,
						"propertyNames":        map[string]any{"pattern": "^[a-z][a-z0-9_]{0,30}$"},
						"additionalProperties": map[string]any{"type": "string", "pattern": "^/", "description": "RFC 6901 JSON pointer into the JSON response body."},
						"description":          "Variable name -> pointer. A missing value fails the step. Later steps of the same journey may use {{name}}. The name uniq is built in and unique per virtual user and iteration.",
					},
				},
			},
		},
	}
	out, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		panic("journey: schema does not marshal: " + err.Error())
	}
	return string(out) + "\n"
}
