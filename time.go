package fairgate

import (
	"encoding/json"
	"time"
)

// Time is a [time.Time] that decodes JSON null and "" as the zero time.
type Time struct {
	time.Time
}

// UnmarshalJSON implements [json.Unmarshaler].
func (t *Time) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "null", `""`:
		return nil
	}

	return json.Unmarshal(data, &t.Time)
}
