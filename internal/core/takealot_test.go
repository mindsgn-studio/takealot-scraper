package core

import (
	"testing"
)

func TestTakalot(t *testing.T) {

	want := Item{
		Link:          "https://www.takealot.com/midea-6kg-front-loader-1000rpm-titanium/PLID93155744",
		Title:         "Midea 6kg Front Loader 1000rpm - Titanium",
		Source_Name:   "takealot",
		Current_Price: 0,
	}

	got, err := OpenPageTakealot(want.Link)
	if err != nil {
		t.Error(err)
	}

	if got.Link != want.Link {
		t.Errorf("got %q want %q", got.Link, want.Link)
	}

	if got.Title != want.Title {
		t.Errorf("got %q want %q", got.Title, want.Title)
	}

	if got.Current_Price == 0 {
		t.Errorf("price not collected")
	}

	if got.Overall_Rating == 0 {
		t.Errorf("rating not collected")
	}
}
