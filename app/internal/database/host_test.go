package database_test

import (
	"testing"

	"github.com/jovulic/zfsilo/app/internal/database"
	"gorm.io/datatypes"
)

func TestHost_IQN(t *testing.T) {
	tests := []struct {
		name        string
		identifiers []string
		want        string
		wantErr     bool
	}{
		{
			name:        "nominal",
			identifiers: []string{"iqn.2003-01.org.linux-iscsi.give"},
			want:        "iqn.2003-01.org.linux-iscsi.give",
			wantErr:     false,
		},
		{
			name:        "multiple identifiers",
			identifiers: []string{"other-id", "iqn.2003-01.org.linux-iscsi.give"},
			want:        "iqn.2003-01.org.linux-iscsi.give",
			wantErr:     false,
		},
		{
			name:        "no iqn",
			identifiers: []string{"other-id"},
			want:        "",
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &database.Host{Identifiers: datatypes.JSONSlice[string](tt.identifiers)}
			got, err := h.IQN()
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Host.IQN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHost_VolumeIQN(t *testing.T) {
	tests := []struct {
		name       string
		baseIQN    string
		volumeName string
		want       string
		wantErr    bool
	}{
		{
			name:       "nominal",
			baseIQN:    "iqn.2003-01.org.linux-iscsi.give",
			volumeName: "tank-ivol",
			want:       "iqn.2003-01.org.linux-iscsi.give:tank-ivol",
			wantErr:    false,
		},
		{
			name:       "with underscore",
			baseIQN:    "iqn.2003-01.org.linux-iscsi.give",
			volumeName: "tank_ivol",
			want:       "iqn.2003-01.org.linux-iscsi.give:tank-ivol",
			wantErr:    false,
		},
		{
			name:       "no base iqn",
			baseIQN:    "",
			volumeName: "tank-ivol",
			want:       "",
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := datatypes.JSONSlice[string]{}
			if tt.baseIQN != "" {
				ids = append(ids, tt.baseIQN)
			}
			tr := &database.Host{Identifiers: ids}
			got, err := tr.VolumeIQN(tt.volumeName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Host.VolumeIQN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHost_NQN(t *testing.T) {
	tests := []struct {
		name        string
		identifiers []string
		want        string
		wantErr     bool
	}{
		{
			name:        "nominal",
			identifiers: []string{"nqn.2014-08.org.nvmexpress:give"},
			want:        "nqn.2014-08.org.nvmexpress:give",
			wantErr:     false,
		},
		{
			name:        "no nqn",
			identifiers: []string{"other-id"},
			want:        "",
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &database.Host{Identifiers: datatypes.JSONSlice[string](tt.identifiers)}
			got, err := h.NQN()
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Host.NQN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHost_VolumeNQN(t *testing.T) {
	tests := []struct {
		name       string
		baseNQN    string
		volumeName string
		want       string
		wantErr    bool
	}{
		{
			name:       "nominal",
			baseNQN:    "nqn.2014-08.org.nvmexpress:give",
			volumeName: "tank-ivol",
			want:       "nqn.2014-08.org.nvmexpress:give:tank-ivol",
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := datatypes.JSONSlice[string]{}
			if tt.baseNQN != "" {
				ids = append(ids, tt.baseNQN)
			}
			tr := &database.Host{Identifiers: ids}
			got, err := tr.VolumeNQN(tt.volumeName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Host.VolumeNQN() = %v, want %v", got, tt.want)
			}
		})
	}
}
