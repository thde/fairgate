package fairgate

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTime_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:    "valid RFC3339 timestamp",
			input:   `"2024-01-15T10:30:00Z"`,
			want:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "valid RFC3339 timestamp with timezone",
			input:   `"2024-01-15T10:30:00+01:00"`,
			want:    time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "valid RFC3339 timestamp with nanoseconds",
			input:   `"2024-01-15T10:30:00.123456789Z"`,
			want:    time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC),
			wantErr: false,
		},
		{
			name:    "null value",
			input:   `null`,
			want:    time.Time{},
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   `""`,
			want:    time.Time{},
			wantErr: false,
		},
		{
			name:    "invalid timestamp format",
			input:   `"not-a-timestamp"`,
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			want:    time.Time{},
			wantErr: true,
		},
		{
			name:    "number instead of string",
			input:   `1234567890`,
			want:    time.Time{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Time
			err := json.Unmarshal([]byte(tt.input), &got)

			if (err != nil) != tt.wantErr {
				t.Errorf("Time.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !got.Equal(tt.want) {
				t.Errorf("Time.UnmarshalJSON() = %v, want %v", got.Time, tt.want)
			}
		})
	}
}

func TestTime_UnmarshalJSON_InStruct(t *testing.T) {
	type TestStruct struct {
		Date Time `json:"date"`
	}

	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:    "struct with valid date",
			input:   `{"date":"2024-01-15T10:30:00Z"}`,
			want:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "struct with null date",
			input:   `{"date":null}`,
			want:    time.Time{},
			wantErr: false,
		},
		{
			name:    "struct with empty string date",
			input:   `{"date":""}`,
			want:    time.Time{},
			wantErr: false,
		},
		{
			name:    "struct with missing date field",
			input:   `{}`,
			want:    time.Time{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got TestStruct
			err := json.Unmarshal([]byte(tt.input), &got)

			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !got.Date.Equal(tt.want) {
				t.Errorf("Time = %v, want %v", got.Date, tt.want)
			}
		})
	}
}

func TestTime_IsZero(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "null value should be zero",
			input: `null`,
			want:  true,
		},
		{
			name:  "empty string should be zero",
			input: `""`,
			want:  true,
		},
		{
			name:  "valid timestamp should not be zero",
			input: `"2024-01-15T10:30:00Z"`,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tm Time
			_ = json.Unmarshal([]byte(tt.input), &tm)

			if got := tm.IsZero(); got != tt.want {
				t.Errorf("Time.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}
