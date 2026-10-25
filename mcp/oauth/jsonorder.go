package oauth

// jsonMember is one member of an object whose key order is the reference's
// construction order.
type jsonMember struct {
	name  string
	value any
}

// marshalOrderedJSONObject encodes members in order with the reference's
// JSON.stringify escaping (no HTML escaping) instead of sorting map keys.
func marshalOrderedJSONObject(members []jsonMember) ([]byte, error) {
	encoded := make([]byte, 0, 128)
	encoded = append(encoded, '{')
	for index, member := range members {
		if index > 0 {
			encoded = append(encoded, ',')
		}
		name, err := marshalJSON(member.name)
		if err != nil {
			return nil, err
		}
		value, err := marshalJSON(member.value)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, name...)
		encoded = append(encoded, ':')
		encoded = append(encoded, value...)
	}
	return append(encoded, '}'), nil
}
