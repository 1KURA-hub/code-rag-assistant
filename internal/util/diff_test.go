package util

import (
	"reflect"
	"testing"
)

func TestExtractDiffPaths(t *testing.T) {
	diff := `diff --git a/mq/consumer.go b/mq/consumer.go
--- a/mq/consumer.go
+++ b/mq/consumer.go
@@ -1,3 +1,3 @@
- old retry behavior
+ new retry behavior
diff --git a/service/select.go b/service/select.go
--- a/service/select.go
+++ b/service/select.go`

	got := ExtractDiffPaths(diff)
	want := []string{"mq/consumer.go", "service/select.go"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractDiffPaths() = %v, want %v", got, want)
	}
}

func TestExtractDiffHintsIncludesPathPartsAndIdentifiers(t *testing.T) {
	diff := `diff --git a/mq/consumer.go b/mq/consumer.go
--- a/mq/consumer.go
+++ b/mq/consumer.go
+func handleRetryMessage() error {
+    return retryDeadLetter()
+}`

	hints := ExtractDiffHints(diff)

	for _, want := range []string{"mq/consumer.go", "consumer", "handleRetryMessage", "retryDeadLetter"} {
		if !contains(hints, want) {
			t.Fatalf("ExtractDiffHints() = %v, want to contain %q", hints, want)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
