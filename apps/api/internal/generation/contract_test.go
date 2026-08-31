package generation

import (
	"encoding/json"
	"testing"
)

func TestStreamMessageFields(t *testing.T) {
	message := StreamMessage{
		SchemaVersion: MessageVersion,
		EventType:     JobCreatedEvent,
		JobID:         "f0ab13fc-8ab1-4dd5-9416-c9861c659fca",
		UserID:        "05414806-56e5-432d-9ea4-f641b43cab58",
		Attempt:       1,
		RequestID:     "req-test",
		CreatedAt:     "2026-08-23T12:00:00Z",
	}
	fields := message.Fields()
	for _, key := range []string{"schema_version", "event_type", "job_id", "user_id", "attempt", "request_id", "created_at"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("stream field %q is missing", key)
		}
	}
	if _, ok := fields["prompt"]; ok {
		t.Fatal("stream message must not contain full prompt")
	}
}

func TestSensitiveJobFieldsAreNotSerialized(t *testing.T) {
	job := GenerationJob{
		ID: "job-id", UserID: "user-id", IdempotencyKey: "secret-key", RequestHash: "hash",
		ProviderJobID: ptr("provider-id"), ProviderPayload: json.RawMessage(`{"secret":"value"}`),
		ErrorCode: ptr("INTERNAL"), ErrorMessage: ptr("internal detail"),
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-key", "provider-id", "internal detail", `"secret"`} {
		if contains(string(raw), secret) {
			t.Fatalf("serialized job leaks %q: %s", secret, raw)
		}
	}
}

func ptr(value string) *string { return &value }

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
