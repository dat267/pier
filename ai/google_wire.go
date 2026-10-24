package ai

import "encoding/json"

// The Developer API SDK rebuilds direct inlineData and functionCall objects in
// field order. Vertex passes those objects through. Neither rewrites the image
// parts nested in functionResponse, so those retain their declaration order.
// Reference: @google/genai 2.21.0 contentToMldev, partToMldev, blobToMldev and
// functionCallToMldev, used by pi v1.4.0 api/google-generative-ai.ts.
func marshalGoogleDeveloperRequest(params *GoogleGenerateContentParams) ([]byte, error) {
	type content struct {
		Parts []googleDeveloperPart `json:"parts"`
		Role  string                `json:"role"`
	}
	contents := make([]content, len(params.Contents))
	for i, original := range params.Contents {
		parts := make([]googleDeveloperPart, len(original.Parts))
		for j, part := range original.Parts {
			parts[j] = googleDeveloperPart(part)
		}
		contents[i] = content{Parts: parts, Role: original.Role}
	}
	type plainParams GoogleGenerateContentParams
	return MarshalJSON(struct {
		Contents []content `json:"contents"`
		*plainParams
	}{contents, (*plainParams)(params)})
}

type googleDeveloperPart GooglePart

// Empty text with a signature is still a text part, not a signature-only part.
// The SDK preserves the explicit empty string produced by convertMessages.
func (p GooglePart) MarshalJSON() ([]byte, error) {
	type plainPart GooglePart
	if p.Text == "" && p.FunctionCall == nil && p.FunctionResponse == nil && p.InlineData == nil {
		return MarshalJSON(struct {
			Text string `json:"text"`
			plainPart
		}{p.Text, plainPart(p)})
	}
	return MarshalJSON(plainPart(p))
}

func (p googleDeveloperPart) MarshalJSON() ([]byte, error) {
	var text *string
	if p.Text != "" || (p.FunctionCall == nil && p.FunctionResponse == nil && p.InlineData == nil) {
		text = &p.Text
	}
	var call, image any
	if p.FunctionCall != nil {
		call = struct {
			Args json.RawMessage `json:"args,omitempty"`
			ID   string          `json:"id,omitempty"`
			Name string          `json:"name"`
		}{p.FunctionCall.Args, p.FunctionCall.ID, p.FunctionCall.Name}
	}
	if p.InlineData != nil {
		image = struct {
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		}{p.InlineData.Data, p.InlineData.MimeType}
	}
	return MarshalJSON(struct {
		FunctionCall     any                     `json:"functionCall,omitempty"`
		FunctionResponse *GoogleFunctionResponse `json:"functionResponse,omitempty"`
		InlineData       any                     `json:"inlineData,omitempty"`
		Text             *string                 `json:"text,omitempty"`
		Thought          *bool                   `json:"thought,omitempty"`
		ThoughtSignature *string                 `json:"thoughtSignature,omitempty"`
	}{call, p.FunctionResponse, image, text, p.Thought, p.ThoughtSignature})
}
