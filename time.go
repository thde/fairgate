package fairgate

import (
	"encoding/json"
	"time"
)

// Time supports unmarshalling times returned by the Fairgate API.
type Time struct {
	time.Time
}

// UnmarshalJSON implements the [json.Unmarshaler] interface.
func (m *Time) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		return nil
	}

	return json.Unmarshal(data, &m.Time)
}
