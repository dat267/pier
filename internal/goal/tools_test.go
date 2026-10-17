package goal

import (
	"reflect"
	"testing"
)

func TestDesiredToolNamesAlwaysExposesSetGoal(t *testing.T) {
	got := DesiredToolNames([]string{"read", "bash"}, false)
	want := []string{"read", "bash", "set_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDesiredToolNamesAddsGetGoalWhilePursuing(t *testing.T) {
	got := DesiredToolNames([]string{"read", "set_goal"}, true)
	want := []string{"read", "set_goal", "get_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDesiredToolNamesDropsGetGoalWhenNotPursuing(t *testing.T) {
	got := DesiredToolNames([]string{"read", "get_goal", "set_goal"}, false)
	want := []string{"read", "set_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDesiredToolNamesPreservesOrderAndDeduplicates(t *testing.T) {
	got := DesiredToolNames([]string{"read", "read", "bash"}, true)
	want := []string{"read", "bash", "set_goal", "get_goal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
