package durable

// Port of tasks.ts.

// DefineTask wraps an executable task definition (upstream defineTask).
func DefineTask(definition TaskDefinition) Task {
	return Task{Definition: definition}
}
