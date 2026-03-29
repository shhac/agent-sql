package errors

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewCreatesQueryError(t *testing.T) {
	qe := New("something broke", FixableByHuman)

	if qe.Message != "something broke" {
		t.Errorf("Message = %q, want %q", qe.Message, "something broke")
	}
	if qe.FixableBy != FixableByHuman {
		t.Errorf("FixableBy = %q, want %q", qe.FixableBy, FixableByHuman)
	}
	if qe.Cause != nil {
		t.Errorf("Cause should be nil, got %v", qe.Cause)
	}
}

func TestWrapWrapsError(t *testing.T) {
	cause := fmt.Errorf("underlying problem")
	qe := Wrap(cause, FixableByRetry)

	if qe.Message != "underlying problem" {
		t.Errorf("Message = %q, want %q", qe.Message, "underlying problem")
	}
	if qe.FixableBy != FixableByRetry {
		t.Errorf("FixableBy = %q, want %q", qe.FixableBy, FixableByRetry)
	}
	if qe.Cause != cause {
		t.Error("Cause should be the original error")
	}
}

func TestWithHintAddsHint(t *testing.T) {
	qe := New("err", FixableByAgent).WithHint("try this")

	if qe.Hint != "try this" {
		t.Errorf("Hint = %q, want %q", qe.Hint, "try this")
	}
}

func TestClassifyPassesThroughPreClassified(t *testing.T) {
	original := New("pre-classified", FixableByHuman)
	result := Classify(original, ErrorContext{})

	if result != original {
		t.Error("Classify should return the same QueryError for pre-classified errors")
	}
}

func TestClassifyFallsToGenericAgentError(t *testing.T) {
	plain := fmt.Errorf("some random error")
	result := Classify(plain, ErrorContext{})

	if result.FixableBy != FixableByAgent {
		t.Errorf("FixableBy = %q, want %q", result.FixableBy, FixableByAgent)
	}
	if result.Message != "some random error" {
		t.Errorf("Message = %q, want %q", result.Message, "some random error")
	}
}

func TestAsReturnsFalseForNil(t *testing.T) {
	var target *QueryError
	if As(nil, &target) {
		t.Error("As(nil) should return false")
	}
}

func TestAsExtractsQueryError(t *testing.T) {
	original := New("test", FixableByHuman)
	var target *QueryError
	if !As(original, &target) {
		t.Fatal("As should return true for QueryError")
	}
	if target != original {
		t.Error("As should extract the same QueryError")
	}
}

func TestNotFoundFormatsWithAliases(t *testing.T) {
	qe := NotFound("ghost", []string{"prod", "staging"})

	if !strings.Contains(qe.Message, "ghost") {
		t.Error("message should contain the alias")
	}
	if !strings.Contains(qe.Message, "prod") {
		t.Error("message should contain available aliases")
	}
	if !strings.Contains(qe.Message, "staging") {
		t.Error("message should contain available aliases")
	}
}

func TestNotFoundFormatsWithEmptyAliases(t *testing.T) {
	qe := NotFound("ghost", nil)

	if !strings.Contains(qe.Message, "ghost") {
		t.Error("message should contain the alias")
	}
	if !strings.Contains(qe.Message, "(none configured)") {
		t.Errorf("message should say none configured, got: %s", qe.Message)
	}
}

func TestQueryErrorErrorReturnsMessage(t *testing.T) {
	qe := New("my message", FixableByAgent)
	if qe.Error() != "my message" {
		t.Errorf("Error() = %q, want %q", qe.Error(), "my message")
	}
}

func TestQueryErrorUnwrapReturnsCause(t *testing.T) {
	cause := fmt.Errorf("root cause")
	qe := Wrap(cause, FixableByRetry)

	if qe.Unwrap() != cause {
		t.Error("Unwrap() should return the cause")
	}

	// No cause
	qe2 := New("no cause", FixableByAgent)
	if qe2.Unwrap() != nil {
		t.Error("Unwrap() should return nil when no cause")
	}
}
