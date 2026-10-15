package goal

import (
	"reflect"
	"testing"
)

func TestDesiredToolNamesAlwaysExposesCreateGoal(t *testing.T) {
	got := DesiredToolNames([]string{"read", "bash"}, false)
	want := []string{"read", "bash", "create_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDesiredToolNamesAddsGoalToolsWhilePursuing(t *testing.T) {
	got := DesiredToolNames([]string{"read", "create_goal"}, true)
	want := []string{"read", "create_goal", "get_goal", "update_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDesiredToolNamesDropsGoalToolsWhenNotPursuing(t *testing.T) {
	got := DesiredToolNames([]string{"read", "get_goal", "update_goal", "create_goal"}, false)
	want := []string{"read", "create_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDesiredToolNamesPreservesOrderAndDeduplicates(t *testing.T) {
	got := DesiredToolNames([]string{"read", "read", "bash"}, true)
	want := []string{"read", "bash", "create_goal", "get_goal", "update_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
