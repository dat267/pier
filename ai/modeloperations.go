package ai

// Port of the public half of utils/model-operations.ts: the model-type helpers
// re-exported from models.ts. The port's catalog is chat-only, so the type is
// "chat" unless a model explicitly carries one.

// GetModelType returns the model's type; a model without one is a chat model
// (upstream getModelType).
func GetModelType(model *Model) string {
	if model == nil || model.Type == "" {
		return "chat"
	}
	return model.Type
}

// IsModelType reports whether the model is of the given type, including the
// legacy chat models that carry no explicit type (upstream isModelType).
func IsModelType(model *Model, modelType string) bool {
	return GetModelType(model) == modelType
}
