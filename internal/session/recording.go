package session

import (
	"bytes"
	"encoding/json"
)

// SanitizeRecordingLine removes large accumulated fields from assistant delta
// events before they are persisted to disk. Pi still emits the original event;
// this only affects the on-disk session recording.
func SanitizeRecordingLine(line []byte) []byte {
	trimmed := bytes.TrimRight(line, "\r\n")
	if len(bytes.TrimSpace(trimmed)) == 0 {
		return line
	}

	var event map[string]interface{}
	if err := json.Unmarshal(trimmed, &event); err != nil {
		return line
	}
	if !sanitizeAssistantDeltaEvent(event) {
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

func sanitizeAssistantDeltaEvent(event map[string]interface{}) bool {
	ae, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ae == nil {
		return false
	}

	aeType, _ := ae["type"].(string)
	if aeType != "thinking_delta" && aeType != "text_delta" {
		return false
	}

	hadHeavyFields := false
	if _, ok := ae["partial"]; ok {
		delete(ae, "partial")
		hadHeavyFields = true
	}
	if _, ok := ae["message"]; ok {
		delete(ae, "message")
		hadHeavyFields = true
	}
	return hadHeavyFields
}
