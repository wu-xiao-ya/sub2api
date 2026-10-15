package repository

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestOpsErrorLogInsertDoesNotPersistRequestReplayFields(t *testing.T) {
	disallowedColumns := []string{
		"request_body",
		"request_headers",
		"request_body_truncated",
		"request_body_bytes",
		"is_retryable",
		"retry_count",
		"resolved_retry_id",
	}

	insertSQL := strings.ToLower(insertOpsErrorLogSQL)
	for _, column := range disallowedColumns {
		if strings.Contains(insertSQL, column) {
			t.Fatalf("ops error log insert still references dropped replay column %q", column)
		}
	}

	inputType := reflect.TypeOf(service.OpsInsertErrorLogInput{})
	disallowedFields := []string{
		"RequestBodyJSON",
		"RequestBodyTruncated",
		"RequestBodyBytes",
		"RequestHeadersJSON",
		"IsRetryable",
		"RetryCount",
		"ResolvedRetryID",
	}
	for _, field := range disallowedFields {
		if _, ok := inputType.FieldByName(field); ok {
			t.Fatalf("OpsInsertErrorLogInput still carries replay field %q", field)
		}
	}
}

func TestOpsErrorLogInsertPersistsUsageSource(t *testing.T) {
	if !strings.Contains(strings.ToLower(insertOpsErrorLogSQL), "usage_source") {
		t.Fatal("ops error log insert must persist usage_source")
	}

	args := opsInsertErrorLogArgs(&service.OpsInsertErrorLogInput{UsageSource: "channel_monitor"})
	if len(args) != 39 {
		t.Fatalf("ops error insert args = %d, want 39", len(args))
	}
	got, ok := args[17].(sql.NullString)
	if !ok || !got.Valid || got.String != "channel_monitor" {
		t.Fatalf("usage_source arg = %#v, want valid channel_monitor", args[17])
	}
}
