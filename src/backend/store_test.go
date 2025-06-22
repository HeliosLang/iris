package main

import "testing"

func TestExtractChunkID(t *testing.T) {
	tests := []struct {
		path    string
		want    uint32
		wantErr bool
	}{
		{"00001.secondary", 1, false},
		{"/var/db/00001.secondary", 1, false},
		{"blocks-0002.dat", 2, false},
		{"/tmp/blocks-0002.dat", 2, false},
		{"/files/blocks-0010.other", 10, false},
		{"bad", 0, true},
		{"blocks-xyz.dat", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := extractChunkID(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s", tt.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("for %s got %d want %d", tt.path, got, tt.want)
			}
		})
	}
}
