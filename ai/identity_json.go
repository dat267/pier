package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	object, err := decodeOrderedJSONFields(data)
	if err != nil {
		return nil, fmt.Errorf("ai: client identity must be a JSON object")
	}
	fields := make([]identityJSONField, 0, len(object.keys))
	for _, key := range object.keys {
		fields = append(fields, identityJSONField{key: key, raw: object.values[key]})
	}
	return fields, nil
}

// marshalIdentityJSON re-emits a loaded identity object, applying the fields the
// caller sets and dropping the ones it clears. Members the port does not model
// keep their position, as JavaScript property assignment does.
func marshalIdentityJSON(original []identityJSONField, fields ...identityJSONValue) ([]byte, error) {
	var object orderedJSONFields
	for _, field := range original {
		object.set(field.key, field.raw)
	}
	for _, field := range fields {
		if !field.present {
			object.remove(field.key)
			continue
		}
		raw, err := MarshalJSON(field.value)
		if err != nil {
			return nil, err
		}
		object.set(field.key, raw)
	}
	return object.marshal()
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
