# HomeBox JSON response hardening

HomeBox must not assume that an HTTP 200 response with
`response_format.type = "json_object"` contains bare JSON. Qwen can return a
correct draft inside a Markdown fence, sometimes after explanatory prose.

There are two complementary changes to make in the HomeBox repository:

1. Normalize one unambiguous fenced JSON object before unmarshalling it.
2. Send a JSON schema with the request so llama.cpp constrains generation.

The parser change is still required as a compatibility fallback. Request-side
constraints can be ignored or implemented differently by other OpenAI-compatible
providers.

## 1. Normalize the response before unmarshalling

In `service_ai_capture.go`, find the code that reads the assistant message and
calls `json.Unmarshal` into the AI capture draft. Replace any start-only fence
removal with a small helper using this contract:

1. Trim surrounding whitespace.
2. If the complete response is a valid JSON object, return it unchanged.
3. Otherwise, inspect JSON-labelled and unlabelled Markdown code fences.
4. Keep only fences whose complete contents are valid JSON objects.
5. Accept exactly one valid fenced JSON object, even with prose before or after
   the fence.
6. Return an error when there are no valid candidates.
7. Return a distinct ambiguity error when there is more than one valid
   candidate, even if the candidates are identical.

Do not recover JSON by slicing from the first `{` to the last `}`. Prose can
contain braces or examples, and combining unrelated fragments can silently
produce the wrong draft.

The following helper is ready to adapt to the package's existing error style:

```go
package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var (
	errNoJSONObject        = errors.New("AI response does not contain a valid JSON object")
	errAmbiguousJSONObjects = errors.New("AI response contains multiple JSON objects")

	jsonFencePattern = regexp.MustCompile(
		"(?is)```(?:json)?[ \\t]*\\r?\\n(.*?)\\r?\\n```[ \\t]*",
	)
)

func isJSONObject(value []byte) bool {
	value = bytes.TrimSpace(value)
	return len(value) > 0 && value[0] == '{' && json.Valid(value)
}

func extractSingleJSONObject(response string) ([]byte, error) {
	trimmed := bytes.TrimSpace([]byte(response))
	if isJSONObject(trimmed) {
		return append([]byte(nil), trimmed...), nil
	}

	matches := jsonFencePattern.FindAllStringSubmatch(response, -1)
	candidates := make([][]byte, 0, len(matches))
	for _, match := range matches {
		candidate := bytes.TrimSpace([]byte(match[1]))
		if isJSONObject(candidate) {
			candidates = append(candidates, append([]byte(nil), candidate...))
		}
	}

	switch len(candidates) {
	case 0:
		return nil, errNoJSONObject
	case 1:
		return candidates[0], nil
	default:
		return nil, fmt.Errorf("%w: found %d", errAmbiguousJSONObjects, len(candidates))
	}
}
```

Use the normalized bytes at the existing unmarshal site:

```go
draftJSON, err := extractSingleJSONObject(completion.Choices[0].Message.Content)
if err != nil {
	return nil, fmt.Errorf("normalize AI capture response: %w", err)
}

var draft aiCaptureDraft
if err := json.Unmarshal(draftJSON, &draft); err != nil {
	return nil, fmt.Errorf("decode AI capture draft: %w", err)
}
```

Keep the repository's actual completion and draft type names. Do not log the
complete model response by default; it can contain user inventory data. The
wrapped error should indicate whether the response was missing JSON, ambiguous,
or incompatible with the draft type.

## 2. Add unit and service tests

Add table-driven tests next to `service_ai_capture.go`. At minimum, cover:

| Response | Expected result |
| --- | --- |
| Bare JSON object | Accepted |
| JSON fence at the start | Accepted |
| Explanatory prose followed by one JSON fence | Accepted |
| One JSON fence followed by explanatory prose | Accepted |
| One valid JSON fence plus a non-JSON code fence | Accepted |
| Two valid fenced JSON objects | Rejected as ambiguous |
| Two identical valid fenced JSON objects | Rejected as ambiguous |
| Malformed JSON in a fence | Rejected |
| Prose containing unfenced JSON | Rejected |
| JSON array, scalar, or `null` | Rejected |

Include the observed regression response as a fixture:

````text
Based on the user instruction and the visible item, here is the corrected JSON response.

```json
{
  "name": "Grey Banana Republic Polo Shirt"
}
```
````

The service-level test should stub the OpenAI-compatible response envelope,
exercise the real capture method, and assert that the parsed draft reaches the
normal success path. It should also verify that two fenced objects produce the
existing user-facing AI-capture error without saving either draft.

## 3. Constrain the request with a schema

The smallest llama.cpp-specific improvement is to add a generic object schema:

```json
{
  "response_format": {
    "type": "json_object",
    "schema": {
      "type": "object"
    }
  }
}
```

A field-level schema matching the existing AI capture draft is better. If the
HomeBox OpenAI client supports the standard structured-output wrapper, use:

```json
{
  "response_format": {
    "type": "json_schema",
    "json_schema": {
      "name": "homebox_ai_capture_draft",
      "strict": true,
      "schema": {
        "type": "object",
        "properties": {
          "replace_with_each_draft_field": {}
        },
        "required": [],
        "additionalProperties": false
      }
    }
  }
}
```

Replace the placeholder with every field and type from the current draft DTO.
Only put genuinely mandatory fields in `required`; retain the DTO's optional
fields as optional schema properties. Prefer `additionalProperties: false` so
unexpected model output fails generation or validation rather than being
silently discarded.

If the client library cannot express llama.cpp's `schema` member or the
`json_schema` wrapper, extend only the request DTO/serialization layer. Do not
add the schema text to the prompt as a substitute for `response_format`; prompt
instructions are not output constraints.

## 4. Verify the complete path

Run the HomeBox unit and service tests, then repeat an actual image capture
through the configured llama.cpp endpoint. Verify all of the following:

- The request contains the schema in the serialized HTTP body.
- The completion is HTTP 200 and `message.content` is a JSON object.
- The draft is parsed and returned to the UI.
- A replay of the prose-plus-fence regression response succeeds.
- A response with two valid JSON fences fails without persisting a draft.
- Existing providers that return bare JSON still follow the unchanged path.

On this server, five plain `json_object` trials all returned fenced JSON. Five
trials with `schema: {"type":"object"}` returned bare valid JSON, and three
field-level strict-schema trials all conformed. The Qwen preset also has
reasoning disabled because llama.cpp has had a known conflict between Qwen
thinking output and grammar enforcement.

References:

- [llama.cpp server structured-output documentation](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md)
- [llama.cpp issue: grammar enforcement with thinking enabled](https://github.com/ggml-org/llama.cpp/issues/20345)
