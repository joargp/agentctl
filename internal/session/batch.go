package session

import (
	"encoding/json"
)

// DeltaKey identifies a batch-able delta event by its sub-type and contentIndex.
type DeltaKey struct {
	AeType       string
	ContentIndex float64
}

// batchableDeltaTypes lists the assistantMessageEvent sub-types whose consecutive
// runs are merged into a single log event.
//
// Only toolcall_delta is batched — it is not rendered live by dump/monitor
// (tool args are streamed as JSON fragments, only shown when complete via
// tool_execution_start). Batching text_delta and thinking_delta would delay
// live output visibility in dump --follow and monitor.
var batchableDeltaTypes = map[string]bool{
	"toolcall_delta": true,
}

// ParseBatchableDelta checks if a sanitized JSON line is a delta event that
// can be batched. Returns the batch key, delta string, parsed event, and true
// if batchable.
func ParseBatchableDelta(line []byte) (DeltaKey, string, map[string]interface{}, bool) {
	var event map[string]interface{}
	if err := json.Unmarshal(line, &event); err != nil {
		return DeltaKey{}, "", nil, false
	}

	eventType, _ := event["type"].(string)
	if eventType != "message_update" {
		// Check for top-level delta events (OpenAI models).
		if batchableDeltaTypes[eventType] {
			delta, _ := event["delta"].(string)
			ci, _ := event["contentIndex"].(float64)
			key := DeltaKey{AeType: eventType, ContentIndex: ci}
			return key, delta, event, true
		}
		return DeltaKey{}, "", nil, false
	}

	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		return DeltaKey{}, "", nil, false
	}

	aeType, _ := ae["type"].(string)
	if !batchableDeltaTypes[aeType] {
		return DeltaKey{}, "", nil, false
	}

	delta, _ := ae["delta"].(string)
	ci, _ := ae["contentIndex"].(float64)
	key := DeltaKey{AeType: aeType, ContentIndex: ci}
	return key, delta, event, true
}

// MarshalBatchedDelta writes a batched delta event with the concatenated delta
// string. Returns the JSON bytes with a trailing newline.
func MarshalBatchedDelta(event map[string]interface{}, delta string) []byte {
	eventType, _ := event["type"].(string)
	if eventType == "message_update" {
		ae, _ := event["assistantMessageEvent"].(map[string]interface{})
		if ae != nil {
			ae["delta"] = delta
		}
	} else {
		event["delta"] = delta
	}
	line, err := json.Marshal(event)
	if err != nil {
		return nil
	}
	return append(line, '\n')
}
