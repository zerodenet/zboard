package handler

import "testing"

func setupStringPtr(value string) *string { return &value }
func setupIntPtr(value int) *int          { return &value }

func TestNormalizeSetupSystemPreferencesUsesServerDefaults(t *testing.T) {
	preferences, err := normalizeSetupSystemPreferences(setupInstallWithPreferencesRequest{})
	if err != nil {
		t.Fatalf("normalize setup preferences: %v", err)
	}
	if preferences.SystemTimezone != defaultSystemTimezone {
		t.Fatalf("timezone = %q, want %q", preferences.SystemTimezone, defaultSystemTimezone)
	}
	if preferences.AuditLogRetentionDays != defaultAuditRetentionDays {
		t.Fatalf("audit retention = %d, want %d", preferences.AuditLogRetentionDays, defaultAuditRetentionDays)
	}
	if preferences.OperationRetentionDays != defaultOperationRetention {
		t.Fatalf("operation retention = %d, want %d", preferences.OperationRetentionDays, defaultOperationRetention)
	}
	if preferences.TaskHistoryRetentionDays != defaultTaskRetentionDays {
		t.Fatalf("task retention = %d, want %d", preferences.TaskHistoryRetentionDays, defaultTaskRetentionDays)
	}
}

func TestNormalizeSetupSystemPreferencesAcceptsIANAAndRetentionBounds(t *testing.T) {
	preferences, err := normalizeSetupSystemPreferences(setupInstallWithPreferencesRequest{
		SystemTimezone:           setupStringPtr("Asia/Shanghai"),
		AuditLogRetentionDays:    setupIntPtr(0),
		OperationRetentionDays:   setupIntPtr(historyRetentionMaxDays),
		TaskHistoryRetentionDays: setupIntPtr(30),
	})
	if err != nil {
		t.Fatalf("normalize setup preferences: %v", err)
	}
	if preferences.SystemTimezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q", preferences.SystemTimezone)
	}
	if preferences.AuditLogRetentionDays != 0 {
		t.Fatalf("audit retention = %d, want 0", preferences.AuditLogRetentionDays)
	}
	if preferences.OperationRetentionDays != historyRetentionMaxDays {
		t.Fatalf("operation retention = %d, want %d", preferences.OperationRetentionDays, historyRetentionMaxDays)
	}
	if preferences.TaskHistoryRetentionDays != 30 {
		t.Fatalf("task retention = %d, want 30", preferences.TaskHistoryRetentionDays)
	}
}

func TestNormalizeSetupSystemPreferencesRejectsInvalidTimezone(t *testing.T) {
	_, err := normalizeSetupSystemPreferences(setupInstallWithPreferencesRequest{
		SystemTimezone: setupStringPtr("UTC+8"),
	})
	if err == nil {
		t.Fatal("expected invalid timezone to be rejected")
	}
}

func TestNormalizeSetupSystemPreferencesRejectsInvalidRetention(t *testing.T) {
	for name, request := range map[string]setupInstallWithPreferencesRequest{
		"negative": {
			AuditLogRetentionDays: setupIntPtr(-1),
		},
		"above max": {
			TaskHistoryRetentionDays: setupIntPtr(historyRetentionMaxDays + 1),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeSetupSystemPreferences(request); err == nil {
				t.Fatal("expected invalid retention to be rejected")
			}
		})
	}
}
