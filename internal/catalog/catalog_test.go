package catalog

import "testing"

// The catalog is hand-maintained data, so structural validation is the only
// thing standing between a typo and a broken page.
func TestValidate_CatalogIsWellFormed(t *testing.T) {
	if problems := Validate(); len(problems) > 0 {
		for _, p := range problems {
			t.Errorf("catalog problem: %s", p)
		}
	}
}

// A help-wanted feature with no issue tells a visitor work exists but gives
// them nothing to claim, which is worse than not listing it.
func TestValidate_HelpWantedRequiresIssue(t *testing.T) {
	for _, f := range ByStatus(HelpWanted) {
		if f.Issue == 0 {
			t.Errorf("%s is help-wanted but references no issue", f.ID)
		}
	}
}

func TestValidate_CatchesBadEntries(t *testing.T) {
	// Validate() reads the package-level Features, so swap it out and restore.
	orig := Features
	defer func() { Features = orig }()

	Features = []Feature{
		{ID: "", Title: "no id", Summary: "x", Status: Shipped, Area: AreaOps},
		{ID: "dup", Title: "a", Summary: "x", Status: Shipped, Area: AreaOps},
		{ID: "dup", Title: "b", Summary: "x", Status: Shipped, Area: AreaOps},
		{ID: "badstatus", Title: "c", Summary: "x", Status: "invented", Area: AreaOps},
		{ID: "badarea", Title: "d", Summary: "x", Status: Shipped, Area: "nowhere"},
		{ID: "helpnoissue", Title: "e", Summary: "x", Status: HelpWanted, Area: AreaOps},
		{ID: "nosummary", Title: "f", Summary: "", Status: Shipped, Area: AreaOps},
	}

	problems := Validate()
	if len(problems) < 6 {
		t.Errorf("got %d problems, want at least 6: %v", len(problems), problems)
	}
}

func TestByStatusAndCounts(t *testing.T) {
	shipped := ByStatus(Shipped)
	if len(shipped) == 0 {
		t.Error("ByStatus(Shipped) is empty; the backend is genuinely shipped")
	}
	for _, f := range shipped {
		if f.Status != Shipped {
			t.Errorf("%s leaked into ByStatus(Shipped) with status %s", f.ID, f.Status)
		}
	}

	counts := Counts()
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != len(Features) {
		t.Errorf("Counts total = %d, want %d", total, len(Features))
	}
}

func TestStatusAndAreaLabels(t *testing.T) {
	if Shipped.Label() != "Shipped" || HelpWanted.Label() != "Help wanted" {
		t.Error("status labels are wrong")
	}
	if AreaIOS.Label() != "iOS" || AreaBackend.Label() != "Backend" {
		t.Error("area labels are wrong")
	}
}
