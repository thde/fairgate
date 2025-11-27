package fairgate

import (
	"encoding/json"
	"time"
)

type Time struct {
	time.Time
}

func (m *Time) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		return nil
	}

	return json.Unmarshal(data, &m.Time)
}
