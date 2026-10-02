package durable

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/chord"
)

// Port of documents.ts: definitions, address resolution and typed-access
// checks.

func conversationDefinition(history string) DocDefinition {
	fork := ForkAsOf
	return DocDefinition{
		Kind: "notes", Version: 1, Scope: ScopeConversation, History: &history, Fork: &fork,
		Initial: func(chord.JsonValue) (chord.JsonValue, error) { return map[string]any{}, nil },
	}
}

func TestDefineDocValidatesVersion(t *testing.T) {
	if _, err := DefineDoc(DocDefinition{Kind: "k", Version: 0, Scope: ScopeSession}); err == nil ||
		!strings.Contains(err.Error(), "version must be a positive integer") {
		t.Fatalf("err = %v", err)
	}
	if _, err := DefineDoc(DocDefinition{Kind: "k", Version: 1, Scope: "other"}); err == nil ||
		!strings.Contains(err.Error(), "unknown scope") {
		t.Fatalf("err = %v", err)
	}
	if _, err := DefineDoc(DocDefinition{Kind: "k", Version: 2, Scope: ScopeSession}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if _, err := DefineDocFamily(DocDefinition{Kind: "k", Version: 1, Scope: ScopeConversation}); err != nil {
		t.Fatalf("family err = %v", err)
	}
}

func TestResolveAddress(t *testing.T) {
	// Session singleton: no arguments.
	session := DocDefinition{Kind: "s", Version: 1, Scope: ScopeSession}
	resolved, err := ResolveAddress(session)
	if err != nil || resolved.NextArgument != 0 || resolved.Address.Scope.Kind != ScopeSession {
		t.Fatalf("session = %+v err=%v", resolved, err)
	}
	if got := AddressID(resolved.Address); got != `["s","session",null,null]` {
		t.Fatalf("address id = %s", got)
	}

	// Conversation singleton: the owner id first.
	conversation, err := ResolveAddress(conversationDefinition(HistoryRewindable), Id(7))
	if err != nil || conversation.NextArgument != 1 ||
		conversation.Address.Scope.ConversationID == nil || *conversation.Address.Scope.ConversationID != 7 {
		t.Fatalf("conversation = %+v err=%v", conversation, err)
	}
	if got := AddressID(conversation.Address); got != `["notes","conversation",7,null]` {
		t.Fatalf("address id = %s", got)
	}

	// Task singleton, and a family key after the owner.
	task := DocDefinition{Kind: "t", Version: 1, Scope: ScopeTask}
	resolved, err = ResolveAddress(task, 9)
	if err != nil || resolved.NextArgument != 1 || resolved.Address.Scope.TaskID == nil {
		t.Fatalf("task = %+v err=%v", resolved, err)
	}
	family := conversationDefinition(HistoryRewindable)
	family.Family = true
	resolved, err = ResolveAddress(family, 7, "member")
	if err != nil || resolved.NextArgument != 2 || resolved.Address.Key == nil || *resolved.Address.Key != "member" {
		t.Fatalf("family = %+v err=%v", resolved, err)
	}
	if got := AddressID(resolved.Address); got != `["notes","conversation",7,"member"]` {
		t.Fatalf("address id = %s", got)
	}

	// Missing or invalid arguments are rejected.
	if _, err := ResolveAddress(conversationDefinition(HistoryLatest)); err == nil {
		t.Fatal("missing owner accepted")
	}
	if _, err := ResolveAddress(conversationDefinition(HistoryLatest), "nope"); err == nil {
		t.Fatal("string owner accepted")
	}
	if _, err := ResolveAddress(family, 7); err == nil {
		t.Fatal("missing family key accepted")
	}
}

func TestDocumentCreateFor(t *testing.T) {
	history, fork := HistoryRewindable, ForkAsOf
	definition := DocDefinition{Kind: "notes", Version: 1, Scope: ScopeConversation, History: &history, Fork: &fork}
	address := DocumentAddress{Kind: "notes", Scope: DocumentScope{Kind: ScopeConversation, ConversationID: idPtr(7)}}
	create := DocumentCreateFor(definition, address, 11)
	if create.ID != 11 || create.History == nil || *create.History != HistoryRewindable ||
		create.Fork == nil || *create.Fork != ForkAsOf {
		t.Fatalf("create = %+v", create)
	}
	sessionCreate := DocumentCreateFor(DocDefinition{Kind: "s", Version: 1, Scope: ScopeSession},
		DocumentAddress{Kind: "s", Scope: DocumentScope{Kind: ScopeSession}}, 12)
	if sessionCreate.History != nil || sessionCreate.Fork != nil {
		t.Fatalf("session create = %+v", sessionCreate)
	}
}

func TestCheckRecordScopeAndVersion(t *testing.T) {
	history, fork := HistoryRewindable, ForkAsOf
	definition := DocDefinition{Kind: "notes", Version: 2, Scope: ScopeConversation, History: &history, Fork: &fork,
		Migrate: func(value chord.JsonValue, fromVersion int) (chord.JsonValue, error) {
			return value, nil
		}}
	create := DocumentCreateFor(definition, DocumentAddress{Kind: "notes",
		Scope: DocumentScope{Kind: ScopeConversation, ConversationID: idPtr(7)}}, 11)
	if err := CheckRecordScope(definition, SemanticsOfCreate(create)); err != nil {
		t.Fatal(err)
	}
	// A different history mode is rejected.
	latest := HistoryLatest
	other := *create
	other.History = &latest
	if err := CheckRecordScope(definition, SemanticsOfCreate(&other)); err == nil ||
		!strings.Contains(err.Error(), "does not match the supplied definition semantics") {
		t.Fatalf("scope err = %v", err)
	}
	// Version checks: newer is rejected, older needs a migration.
	if err := CheckRecordVersion(definition, SemanticsOfCreate(create), 3); err == nil ||
		!strings.Contains(err.Error(), "has newer version") {
		t.Fatalf("newer err = %v", err)
	}
	withoutMigrate := definition
	withoutMigrate.Migrate = nil
	if err := CheckRecordVersion(withoutMigrate, SemanticsOfCreate(create), 1); err == nil ||
		!strings.Contains(err.Error(), "requires migration") {
		t.Fatalf("migration err = %v", err)
	}
	if err := CheckRecordVersion(definition, SemanticsOfCreate(create), 1); err != nil {
		t.Fatalf("migration allowed: %v", err)
	}
}

func TestMaterializeDocumentValueMigrates(t *testing.T) {
	history, fork := HistoryRewindable, ForkAsOf
	definition := DocDefinition{Kind: "notes", Version: 2, Scope: ScopeConversation, History: &history, Fork: &fork,
		Migrate: func(value chord.JsonValue, fromVersion int) (chord.JsonValue, error) {
			object := value.(map[string]any)
			object["migratedFrom"] = float64(fromVersion)
			return object, nil
		}}
	view := DocumentSemanticsView{ID: 11, Kind: "notes", ScopeKind: ScopeConversation, History: &history, Fork: &fork}
	// Current version passes through unchanged.
	current, err := MaterializeDocumentValue(definition, view, 2, json.RawMessage(`{"value":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if current.(map[string]any)["value"] != float64(1) {
		t.Fatalf("current = %+v", current)
	}
	// An older version migrates.
	migrated, err := MaterializeDocumentValue(definition, view, 1, json.RawMessage(`{"value":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if migrated.(map[string]any)["migratedFrom"] != float64(1) {
		t.Fatalf("migrated = %+v", migrated)
	}
	// A stored document materializes through the same path.
	stored := &StoredDocument{Record: DocumentRecord{ID: 11, Kind: "notes",
		Scope: DocumentScope{Kind: ScopeConversation, ConversationID: idPtr(7)}, History: &history, Fork: &fork},
		Version: 1, Value: json.RawMessage(`{"value":2}`)}
	materialized, err := MaterializeDocument(definition, stored)
	if err != nil {
		t.Fatal(err)
	}
	if materialized.(map[string]any)["migratedFrom"] != float64(1) {
		t.Fatalf("stored = %+v", materialized)
	}
}
