package oauth

import "encoding/json"

func unmarshalJSON(data []byte, target any) error {
	return json.Unmarshal(data, target)
}

func marshalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}
