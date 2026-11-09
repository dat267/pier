package goal

import (
	"reflect"
	"testing"
)

func TestDesiredToolNamesExposesBothGoalTools(t *testing.T) {
	got := DesiredToolNames([]string{"read", "bash"})
	want := []string{"read", "bash", "set_goal", "get_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// D418: the goal tools are always active, so a goal transition cannot change
// the tool array or the prompt sections a provider's prefix cache covers.
func TestDesiredToolNamesKeepsGetGoalAfterAGoalEnds(t *testing.T) {
	got := DesiredToolNames([]string{"read", "set_goal", "get_goal"})
	want := []string{"read", "set_goal", "get_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDesiredToolNamesPreservesOrderAndDeduplicates(t *testing.T) {
	got := DesiredToolNames([]string{"read", "read", "bash"})
	want := []string{"read", "bash", "set_goal", "get_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
