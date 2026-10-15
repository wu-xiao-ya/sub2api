package service

import (
	"errors"
	"testing"
)

func TestNormalizeAccountPoolGroup(t *testing.T) {
	tests := []struct {
		name       string
		input      AccountPoolGroup
		wantName   string
		wantStatus string
		wantErr    error
		wantAnyErr bool
	}{
		{
			name:       "defaults active and trims fields",
			input:      AccountPoolGroup{Name: "  AIHub Pro  ", UpstreamKey: "  aihub  ", Description: "  primary  "},
			wantName:   "AIHub Pro",
			wantStatus: AccountPoolGroupStatusActive,
		},
		{
			name:       "allows inactive",
			input:      AccountPoolGroup{Name: "AIHub Plus", Status: AccountPoolGroupStatusInactive},
			wantName:   "AIHub Plus",
			wantStatus: AccountPoolGroupStatusInactive,
		},
		{
			name:    "requires name",
			input:   AccountPoolGroup{Name: "  "},
			wantErr: ErrAccountPoolGroupNameRequired,
		},
		{
			name:       "rejects unknown status",
			input:      AccountPoolGroup{Name: "AIHub", Status: "disabled"},
			wantAnyErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeAccountPoolGroup(tt.input)
			if tt.wantAnyErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Name != tt.wantName {
				t.Fatalf("name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
		})
	}
}
