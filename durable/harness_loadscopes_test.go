package durable

import (
	"context"
	"testing"
)

// Port of the scheduler's scope loading.

func TestLoadChainAndScopes(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var genID, childID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		genID, err = CreateGeneration(tx, RootConversationID)
		if err != nil {
			return err
		}
		childID, err = CreateToolTask(tx, genID, 1, "c1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mirror := NewSchedulerMirror()
	if err := session.Commit(ctx, func(tx *Transaction) error { return LoadLiveTasks(tx, mirror) }); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		if _, err := LoadScopes(false, mirror, storage, ctx); err != nil {
			return err
		}
		// The root conversation's owner edge is learned from Storage.
		owner, present := mirror.Edges[RootConversationID]
		if !present || owner != nil {
			t.Fatalf("edges = %+v", mirror.Edges)
		}
		// The child's chain resolves through the live generation task.
		if err := LoadChain(SchedulerUp{Task: &childID}, mirror, storage, ctx); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A queued input in a new conversation loads its chain and is returned.
	var conversationID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		conversation, err := tx.CreateConversation(ConversationOwnership{Kind: ConversationOwnerless})
		if err != nil {
			return err
		}
		conversationID = conversation.ID
		_, err = tx.CreateSubmission(SubmissionCreate{
			ConversationID: conversation.ID, Type: SubmissionTypeInput, Status: SubmissionQueued,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(tx *Transaction) error {
		conversations, err := LoadScopes(true, mirror, storage, ctx)
		if err != nil {
			return err
		}
		if len(conversations) != 1 || conversations[0] != conversationID {
			t.Fatalf("conversations = %+v", conversations)
		}
		if owner, present := mirror.Edges[conversationID]; !present || owner != nil {
			t.Fatalf("edges = %+v", mirror.Edges)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
