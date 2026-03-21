package session

import (
	"bytes"
	"encoding/json"
)

// SanitizeRecordingLine removes large redundant fields from streaming pi events
// before they are persisted to disk. Pi still emits the original events to the
// terminal; this only affects the on-disk recording.
//
// Pi's streaming protocol attaches accumulated-state snapshots to almost every
// event so that a subscriber joining mid-stream can reconstruct the full message.
// These snapshots grow quadratically with session length — a long coding session
// can produce 500MB+ logs without this sanitization. The individual delta events
// are sufficient to reconstruct all content.
//
// Fields stripped per event type:
//
//   - message_update: top-level "message" snapshot and assistantMessageEvent
//     "partial"/"message" (all carry the full accumulated message so far).
//     Also strips toolcall_end.ae.toolCall (assembled tool input, dup with
//     tool_execution_start.args) and thinking_end.ae.content (assembled thinking
//     text, dup with thinking_delta events).
//
//   - message_end: entire "message" field (assembled content redundant with deltas
//     and with turn_end; no consumer reads it).
//
//   - turn_end: "message.content" array (assembled content redundant with deltas;
//     only message.usage is read for token counts and cost totals).
//
//   - tool_execution_update: "args" (dup of tool_execution_start.args).
//     Note: "partialResult" is preserved — monitor reads it for live output.
//
//   - message_start: "message.content" for non-user roles (assistant, toolResult,
//     custom, system) — never rendered by dump; user content is preserved.
func SanitizeRecordingLine(line []byte) []byte {
	trimmed := bytes.TrimRight(line, "\r\n")
	if len(bytes.TrimSpace(trimmed)) == 0 {
		return line
	}

	var event map[string]interface{}
	if err := json.Unmarshal(trimmed, &event); err != nil {
		return line
	}
	if !sanitizeEvent(event) {
		return line
	}

	sanitized, err := json.Marshal(event)
	if err != nil {
		return line
	}
	if len(line) > 0 && line[len(line)-1] == '\n' {
		sanitized = append(sanitized, '\n')
	}
	return sanitized
}

// sanitizeEvent strips accumulated snapshot fields from streaming events.
// Returns true if any fields were removed (caller must re-marshal).
func sanitizeEvent(event map[string]interface{}) bool {
	eventType, _ := event["type"].(string)
	switch eventType {
	case "message_update":
		return sanitizeMessageUpdate(event)
	case "message_end":
		return sanitizeMessageEnd(event)
	case "turn_end":
		return sanitizeTurnEnd(event)
	case "tool_execution_update":
		return sanitizeToolExecutionUpdate(event)
	case "message_start":
		return sanitizeMessageStart(event)
	}
	return false
}

// sanitizeMessageUpdate strips accumulated snapshot fields from message_update
// events. The top-level "message" field and assistantMessageEvent "partial"/"message"
// all contain the full accumulated assistant message so far — they grow quadratically
// with session length. Only the per-event delta fields are needed for playback.
//
// For toolcall_end sub-events, ae.toolCall is the fully-assembled tool input — it is
// identical to the tool_execution_start.args emitted immediately after, so it too is
// stripped.
func sanitizeMessageUpdate(event map[string]interface{}) bool {
	changed := false

	// Top-level "message" is the full accumulated assistant message snapshot.
	if _, ok := event["message"]; ok {
		delete(event, "message")
		changed = true
	}

	// assistantMessageEvent "partial" and "message" are the same accumulated state
	// nested inside the sub-event.
	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae != nil {
		if _, ok := ae["partial"]; ok {
			delete(ae, "partial")
			changed = true
		}
		if _, ok := ae["message"]; ok {
			delete(ae, "message")
			changed = true
		}
		// toolcall_end.ae.toolCall is the fully assembled tool input JSON.
		// It is redundant with tool_execution_start.args which follows immediately.
		if ae["type"] == "toolcall_end" {
			if _, ok := ae["toolCall"]; ok {
				delete(ae, "toolCall")
				changed = true
			}
		}
		// thinking_end.ae.content is the fully assembled thinking text — already
		// captured by the preceding thinking_delta events. No consumer reads it.
		if ae["type"] == "thinking_end" {
			if _, ok := ae["content"]; ok {
				delete(ae, "content")
				changed = true
			}
		}
	}

	return changed
}

// sanitizeMessageEnd strips the "message" field from message_end events.
// The assembled content is redundant: it is fully reconstructable from the
// individual delta events, and turn_end carries the same content plus token
// usage. No agentctl consumer (dump, monitor, status, costs) reads message_end.message.
func sanitizeMessageEnd(event map[string]interface{}) bool {
	if _, ok := event["message"]; ok {
		delete(event, "message")
		return true
	}
	return false
}

// sanitizeMessageStart strips the content from non-user message_start events.
// dump only renders message_start.message.content when role == "user"; all other
// roles (assistant, toolResult, custom, system) are never displayed. Stripping
// their content avoids recording tool results and context documents that can be
// hundreds of KB but are redundant with tool_execution_end and other events.
// User message content is preserved so dump can show the original task.
func sanitizeMessageStart(event map[string]interface{}) bool {
	msg, _ := event["message"].(map[string]interface{})
	if msg == nil {
		return false
	}
	role, _ := msg["role"].(string)
	if role == "user" {
		return false // preserve user message content for dump display
	}
	if _, ok := msg["content"]; ok {
		delete(msg, "content")
		return true
	}
	return false
}

// sanitizeToolExecutionUpdate strips redundant fields from tool_execution_update events:
//   - "args": identical to the args in the corresponding tool_execution_start event.
//
// Note: "partialResult" is NOT stripped — monitor reads it for live tool output
// streaming (cmd/monitor.go renderJSONLine case "tool_execution_update").
func sanitizeToolExecutionUpdate(event map[string]interface{}) bool {
	if _, ok := event["args"]; ok {
		delete(event, "args")
		return true
	}
	return false
}

// sanitizeTurnEnd strips the "content" array from turn_end.message.
// The full assembled content is already captured by the individual delta events.
// All agentctl consumers (dump, monitor, ls, costs) only read turn_end.message.usage
// for token counts and cost totals — nothing reads message.content.
func sanitizeTurnEnd(event map[string]interface{}) bool {
	msg, _ := event["message"].(map[string]interface{})
	if msg == nil {
		return false
	}
	if _, ok := msg["content"]; ok {
		delete(msg, "content")
		return true
	}
	return false
}
