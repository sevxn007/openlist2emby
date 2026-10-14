package medialibrary

import (
	"testing"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

func TestMergeMediaPeopleKeepsSeasonCastAndAddsEpisodeGuests(t *testing.T) {
	people := mergeMediaPeople(
		[]domain.MediaPerson{
			{ID: 1, Name: "Season actor", Role: "Lead", SortOrder: 0},
			{ID: 2, Name: "Director", Type: "Director", SortOrder: 100},
		},
		[]domain.MediaPerson{
			{ID: 1, Name: "Season actor", Role: "Lead", SortOrder: 0},
			{ID: 3, Name: "Guest actor", Role: "Guest role", SortOrder: 1},
		},
	)
	if len(people) != 3 || people[0].ID != 1 || people[1].ID != 3 || people[2].ID != 2 {
		t.Fatalf("merged people = %#v", people)
	}
	if people[0].Role != "Lead" {
		t.Fatalf("duplicate season role = %q", people[0].Role)
	}
}

func TestFilterPeopleByImageAvailability(t *testing.T) {
	people := []domain.MediaPerson{
		{ID: 1, Name: "Cached", ProfileTag: "sha256"},
		{ID: 2, Name: "Remote", ProfileURL: "https://image.example/person.jpg"},
		{ID: 3, Name: "Missing"},
	}
	if got := filterPeopleByImageAvailability(false, people, true); len(got) != 3 {
		t.Fatalf("disabled filter returned %d people", len(got))
	}
	got := filterPeopleByImageAvailability(true, people, true)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("locally filtered people = %#v", got)
	}
	got = filterPeopleByImageAvailability(true, people, false)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("remotely filtered people = %#v", got)
	}
	merged := mergeMediaPeople(
		[]domain.MediaPerson{{ID: 4, Name: "Shared", SortOrder: 0}},
		[]domain.MediaPerson{{ID: 4, Name: "Shared", ProfileURL: "https://image.example/shared.jpg", SortOrder: 0}},
	)
	merged = filterPeopleByImageAvailability(true, merged, false)
	if len(merged) != 1 || merged[0].ID != 4 {
		t.Fatalf("merged person with an image was filtered: %#v", merged)
	}
}
