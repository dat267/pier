package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Identity is plain JSON upstream (utils/identity.ts). Keep its object order,
// optional-field presence and unknown fields when loading and persisting it.
type identityJSONField struct {
	key string
	raw json.RawMessage
}

type identityJSONValue struct {
	key     string
	value   any
	present bool
}

func decodeIdentityJSON(data []byte) ([]identityJSONField, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, fmt.Errorf("ai: client identity must be a JSON object")
	}
	keys := []string{}
	values := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key := token.(string)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if _, exists := values[key]; !exists {
			keys = appendJSObjectKey(keys, key)
		}
		values[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	fields := make([]identityJSONField, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, identityJSONField{key, values[key]})
	}
	return fields, nil
}

func marshalIdentityJSON(original []identityJSONField, fields ...identityJSONValue) ([]byte, error) {
	keys := []string{}
	values := map[string]json.RawMessage{}
	for _, field := range original {
		keys = appendJSObjectKey(keys, field.key)
		values[field.key] = field.raw
	}
	for _, field := range fields {
		if !field.present {
			delete(values, field.key)
			continue
		}
		raw, err := MarshalJSON(field.value)
		if err != nil {
			return nil, err
		}
		if _, exists := values[field.key]; !exists {
			keys = appendJSObjectKey(keys, field.key)
		}
		values[field.key] = raw
	}
	var out strings.Builder
	out.WriteByte('{')
	first := true
	for _, key := range keys {
		value, exists := values[key]
		if !exists {
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		out.Write(canonicalJSONString(key))
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return []byte(out.String()), nil
}

func (a ClientAttribution) MarshalJSON() ([]byte, error) {
	return marshalIdentityJSON(a.jsonFields,
		identityJSONValue{"name", a.Name, true},
		identityJSONValue{"url", a.URL, true},
		identityJSONValue{"categories", a.Categories, a.Categories != nil},
	)
}

func (a *ClientAttribution) UnmarshalJSON(data []byte) error {
	fields, err := decodeIdentityJSON(data)
	if err != nil {
		return err
	}
	var decoded ClientAttribution
	for _, field := range fields {
		switch field.key {
		case "name":
			err = json.Unmarshal(field.raw, &decoded.Name)
		case "url":
			err = json.Unmarshal(field.raw, &decoded.URL)
		case "categories":
			err = json.Unmarshal(field.raw, &decoded.Categories)
		}
		if err != nil {
			return err
		}
	}
	decoded.jsonFields = fields
	*a = decoded
	return nil
}

func (i *ClientIdentity) rememberJSONKey(key string) {
	for _, field := range i.jsonFields {
		if field.key == key {
			return
		}
	}
	i.jsonFields = append(i.jsonFields, identityJSONField{key: key})
}

func (i ClientIdentity) MarshalJSON() ([]byte, error) {
	var userAgent any = i.UserAgent
	custom := i.CustomUserAgent
	if custom == nil && i.UserAgent != "" && i.UserAgent != "short" && i.UserAgent != "detailed" {
		custom = &i.UserAgent
	}
	if custom != nil {
		encoded, err := marshalIdentityJSON(i.userAgentFields, identityJSONValue{"custom", *custom, true})
		if err != nil {
			return nil, err
		}
		userAgent = json.RawMessage(encoded)
	}
	return marshalIdentityJSON(i.jsonFields,
		identityJSONValue{"userAgent", userAgent, i.UserAgent != "" || custom != nil || i.userAgentSet},
		identityJSONValue{"version", i.Version, i.Version != "" || i.VersionSet},
		identityJSONValue{"attribution", i.Attribution, i.Attribution != nil || i.AttributionSet},
	)
}

func (i *ClientIdentity) UnmarshalJSON(data []byte) error {
	fields, err := decodeIdentityJSON(data)
	if err != nil {
		return err
	}
	var decoded ClientIdentity
	for _, field := range fields {
		switch field.key {
		case "userAgent":
			decoded.userAgentSet = true
			if bytes.HasPrefix(bytes.TrimSpace(field.raw), []byte("{")) {
				decoded.userAgentFields, err = decodeIdentityJSON(field.raw)
				var custom string
				if err == nil {
					for _, modeField := range decoded.userAgentFields {
						if modeField.key == "custom" {
							err = json.Unmarshal(modeField.raw, &custom)
						}
					}
				}
				decoded.CustomUserAgent = &custom
			} else {
				err = json.Unmarshal(field.raw, &decoded.UserAgent)
			}
		case "attribution":
			decoded.AttributionSet = true
			err = json.Unmarshal(field.raw, &decoded.Attribution)
		case "version":
			decoded.VersionSet = true
			err = json.Unmarshal(field.raw, &decoded.Version)
		}
		if err != nil {
			return err
		}
	}
	decoded.jsonFields = fields
	*i = decoded
	return nil
}
