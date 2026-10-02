package durable

import (
	"encoding/json"
	"fmt"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of documents.ts: document definitions, address resolution and the
// typed-access checks the session performs before it hands a stored value to a
// definition.

// CheckpointInfo is the stored replay state a checkpoint predicate sees
// (upstream CheckpointInfo).
type CheckpointInfo struct {
	// DeltasSinceBase counts the deltas already stored after the newest base,
	// excluding the change being evaluated.
	DeltasSinceBase int
}

// DocDefinition is a singleton or keyed-family document definition
// (upstream CommonDocDefinition & DocumentSemantics). Go has no function
// overloading, so one struct covers both shapes and Family selects the
// argument list.
type DocDefinition struct {
	// Kind is the stable persisted kind.
	Kind string
	// Version is the positive version of the stored value shape.
	Version int
	// Scope is ScopeSession, ScopeConversation or ScopeTask.
	Scope string
	// History and Fork apply to conversation documents (see the History*/
	// Fork* constants).
	History *string
	Fork    *string
	// Family marks a keyed family; its Initial takes the member seed.
	Family bool
	// Initial builds the value of a new incarnation (the seed for a family).
	Initial func(seed chord.JsonValue) (chord.JsonValue, error)
	// Migrate upgrades a value stored at an older version.
	Migrate func(value chord.JsonValue, fromVersion int) (chord.JsonValue, error)
	// CheckpointWhen asks whether an ordinary change should be stored as a
	// complete base instead of a delta.
	CheckpointWhen func(value chord.JsonValue, ops []delta.Op, info CheckpointInfo) bool
}

// DocToken is a typed document token (upstream DocToken/DocFamilyToken).
type DocToken struct {
	Definition DocDefinition
}

// DefineDoc validates a singleton document definition (upstream defineDoc).
func DefineDoc(definition DocDefinition) (DocToken, error) {
	if err := validateDocDefinition(definition); err != nil {
		return DocToken{}, err
	}
	return DocToken{Definition: definition}, nil
}

// DefineDocFamily validates a keyed family definition (upstream
// defineDocFamily).
func DefineDocFamily(definition DocDefinition) (DocToken, error) {
	definition.Family = true
	if err := validateDocDefinition(definition); err != nil {
		return DocToken{}, err
	}
	return DocToken{Definition: definition}, nil
}

// validateDocDefinition mirrors upstream validateDefinition.
func validateDocDefinition(definition DocDefinition) error {
	if definition.Version < 1 {
		return fmt.Errorf("Document %s version must be a positive integer", definition.Kind)
	}
	switch definition.Scope {
	case ScopeSession, ScopeConversation, ScopeTask:
	default:
		return fmt.Errorf("Document %s has an unknown scope %q", definition.Kind, definition.Scope)
	}
	return nil
}

// ResolvedAddress is a logical address with its string identity (upstream
// ResolvedAddress).
type ResolvedAddress struct {
	Address DocumentAddress
	// ID is the stable string identity of the address.
	ID string
	// NextArgument is the index after the owner and family key.
	NextArgument int
}

// ResolveAddress resolves the argument list for a definition: a conversation
// or task document consumes its owner id, a family consumes its key (upstream
// resolveAddress).
func ResolveAddress(definition DocDefinition, args ...any) (ResolvedAddress, error) {
	index := 0
	scope := DocumentScope{}
	switch definition.Scope {
	case ScopeSession:
		scope = DocumentScope{Kind: ScopeSession}
	case ScopeConversation:
		if index >= len(args) {
			return ResolvedAddress{}, fmt.Errorf("Document %s requires a conversation ID", definition.Kind)
		}
		id, ok := docOwnerID(args[index])
		if !ok {
			return ResolvedAddress{}, fmt.Errorf("Document %s requires a conversation ID", definition.Kind)
		}
		index++
		scope = DocumentScope{Kind: ScopeConversation, ConversationID: &id}
	case ScopeTask:
		if index >= len(args) {
			return ResolvedAddress{}, fmt.Errorf("Document %s requires a task ID", definition.Kind)
		}
		id, ok := docOwnerID(args[index])
		if !ok {
			return ResolvedAddress{}, fmt.Errorf("Document %s requires a task ID", definition.Kind)
		}
		index++
		scope = DocumentScope{Kind: ScopeTask, TaskID: &id}
	}
	var key *string
	if definition.Family {
		if index >= len(args) {
			return ResolvedAddress{}, fmt.Errorf("Document %s requires a family key", definition.Kind)
		}
		value, ok := args[index].(string)
		if !ok {
			return ResolvedAddress{}, fmt.Errorf("Document %s requires a family key", definition.Kind)
		}
		index++
		key = &value
	}
	address := DocumentAddress{Kind: definition.Kind, Scope: scope, Key: key}
	return ResolvedAddress{Address: address, ID: AddressID(address), NextArgument: index}, nil
}

// docOwnerID validates an owner id argument.
func docOwnerID(value any) (Id, bool) {
	switch typed := value.(type) {
	case Id:
		return typed, true
	case int:
		return Id(typed), true
	}
	return 0, false
}

// AddressID is the stable string identity of one logical address (upstream
// addressId): [kind, scope kind, owner, key|null].
func AddressID(address DocumentAddress) string {
	var owner any
	switch address.Scope.Kind {
	case ScopeSession:
		owner = nil
	case ScopeConversation:
		if address.Scope.ConversationID != nil {
			owner = *address.Scope.ConversationID
		}
	case ScopeTask:
		if address.Scope.TaskID != nil {
			owner = *address.Scope.TaskID
		}
	}
	var key any
	if address.Key != nil {
		key = *address.Key
	}
	encoded, _ := json.Marshal([]any{address.Kind, address.Scope.Kind, owner, key})
	return string(encoded)
}

// DocumentCreateFor builds the storage create record for a new incarnation at
// an address (upstream documentCreate). Conversation documents carry the
// definition's history and fork modes.
func DocumentCreateFor(definition DocDefinition, address DocumentAddress, id Id) *DocumentCreate {
	create := &DocumentCreate{ID: id, Kind: address.Kind, Key: address.Key, Scope: address.Scope}
	if address.Scope.Kind == ScopeConversation {
		create.History = definition.History
		create.Fork = definition.Fork
	}
	return create
}

// DocumentSemanticsView is the persisted identity and semantics a typed-access
// check sees (upstream checkRecordScope/checkRecordVersion take the record).
type DocumentSemanticsView struct {
	ID        Id
	Kind      string
	ScopeKind string
	History   *string
	Fork      *string
}

// SemanticsOfCreate views a create record.
func SemanticsOfCreate(record *DocumentCreate) DocumentSemanticsView {
	return DocumentSemanticsView{
		ID: record.ID, Kind: record.Kind, ScopeKind: record.Scope.Kind,
		History: record.History, Fork: record.Fork,
	}
}

// SemanticsOfRecord views a persisted record.
func SemanticsOfRecord(record *DocumentRecord) DocumentSemanticsView {
	return DocumentSemanticsView{
		ID: record.ID, Kind: record.Kind, ScopeKind: record.Scope.Kind,
		History: record.History, Fork: record.Fork,
	}
}

// CheckRecordScope rejects typed access whose definition disagrees with the
// persisted scope, history or fork semantics (upstream checkRecordScope).
func CheckRecordScope(definition DocDefinition, view DocumentSemanticsView) error {
	if view.ScopeKind != definition.Scope {
		return fmt.Errorf("Document %d (%s) does not match the supplied definition semantics", view.ID, view.Kind)
	}
	if view.ScopeKind == ScopeConversation &&
		(!sameOptionalString(view.History, definition.History) || !sameOptionalString(view.Fork, definition.Fork)) {
		return fmt.Errorf("Document %d (%s) does not match the supplied definition semantics", view.ID, view.Kind)
	}
	return nil
}

// CheckRecordVersion rejects typed access to a stored version the definition
// cannot use (upstream checkRecordVersion).
func CheckRecordVersion(definition DocDefinition, view DocumentSemanticsView, version int) error {
	if version > definition.Version {
		return fmt.Errorf("Document %d (%s) has newer version %d than %d", view.ID, view.Kind, version, definition.Version)
	}
	if version < definition.Version && definition.Migrate == nil {
		return fmt.Errorf("Document %d (%s) requires migration from version %d", view.ID, view.Kind, version)
	}
	return nil
}

// MaterializeDocument validates and materializes a detached stored value
// (upstream materializeDocument).
func MaterializeDocument(definition DocDefinition, stored *StoredDocument) (chord.JsonValue, error) {
	if stored == nil {
		return nil, nil
	}
	return MaterializeDocumentValue(definition, SemanticsOfRecord(&stored.Record), stored.Version, stored.Value)
}

// MaterializeDocumentValue validates and materializes one detached value
// before its first persisted incarnation (upstream materializeDocumentValue).
func MaterializeDocumentValue(
	definition DocDefinition,
	view DocumentSemanticsView,
	version int,
	value json.RawMessage,
) (chord.JsonValue, error) {
	if err := CheckRecordScope(definition, view); err != nil {
		return nil, err
	}
	if err := CheckRecordVersion(definition, view, version); err != nil {
		return nil, err
	}
	var decoded any
	if err := unmarshalBytes(value, &decoded); err != nil {
		return nil, err
	}
	if version == definition.Version {
		return decoded, nil
	}
	return definition.Migrate(decoded, version)
}
