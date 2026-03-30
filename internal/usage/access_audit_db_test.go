package usage

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestInsertAndQueryManagementAccessLogs(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{StoreContent: false})

	err := InsertManagementAccessLog(ManagementAccessLog{
		ClientIP:     "10.0.0.10",
		ForwardedFor: "198.51.100.1",
		UserAgent:    "Mozilla/5.0",
		Method:       "GET",
		Path:         "/v0/management/usage/logs",
		StatusCode:   200,
		Allowed:      true,
		AuthSubject:  "config-secret",
	})
	if err != nil {
		t.Fatalf("InsertManagementAccessLog(allowed) error = %v", err)
	}
	err = InsertManagementAccessLog(ManagementAccessLog{
		ClientIP:    "10.0.0.11",
		Method:      "GET",
		Path:        "/v0/management/config",
		StatusCode:  401,
		Allowed:     false,
		AuthSubject: "missing-key",
	})
	if err != nil {
		t.Fatalf("InsertManagementAccessLog(denied) error = %v", err)
	}

	allLogs, err := QueryManagementAccessLogs(ManagementAccessLogQueryParams{Page: 1, Size: 10, Days: 1})
	if err != nil {
		t.Fatalf("QueryManagementAccessLogs(all) error = %v", err)
	}
	if got := len(allLogs.Items); got != 2 {
		t.Fatalf("len(allLogs.Items) = %d, want 2", got)
	}

	allowedLogs, err := QueryManagementAccessLogs(ManagementAccessLogQueryParams{
		Page:    1,
		Size:    10,
		Days:    1,
		Allowed: "allowed",
	})
	if err != nil {
		t.Fatalf("QueryManagementAccessLogs(allowed) error = %v", err)
	}
	if got := len(allowedLogs.Items); got != 1 {
		t.Fatalf("len(allowedLogs.Items) = %d, want 1", got)
	}
	if !allowedLogs.Items[0].Allowed || allowedLogs.Items[0].AuthSubject != "config-secret" {
		t.Fatalf("unexpected allowed log row: %+v", allowedLogs.Items[0])
	}

	deniedLogs, err := QueryManagementAccessLogs(ManagementAccessLogQueryParams{
		Page:    1,
		Size:    10,
		Days:    1,
		Allowed: "denied",
	})
	if err != nil {
		t.Fatalf("QueryManagementAccessLogs(denied) error = %v", err)
	}
	if got := len(deniedLogs.Items); got != 1 {
		t.Fatalf("len(deniedLogs.Items) = %d, want 1", got)
	}
	if deniedLogs.Items[0].Allowed || deniedLogs.Items[0].AuthSubject != "missing-key" {
		t.Fatalf("unexpected denied log row: %+v", deniedLogs.Items[0])
	}
}
