package main

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func hasCard(cards []DashboardCard, id string) bool {
	for _, c := range cards {
		if c.ID == id {
			return true
		}
	}
	return false
}

// The LastRank Review card is gated on manage_members, matching the review
// endpoints: only someone who can ACT on a queued decision should be told there
// are decisions waiting. Once the scheduler is on, the queue fills from runs
// nobody watched, so this card may be the only place an officer finds out.
func TestLastRankReviewCardRequiresManageMembers(t *testing.T) {
	withPerm := allowedCards(PageData{Permissions: RankPermissions{ManageMembers: true}})
	if !hasCard(withPerm, "lastrank-review") {
		t.Error("manage_members should see the LastRank Review card")
	}

	without := allowedCards(PageData{Permissions: RankPermissions{ManageMembers: false}})
	if hasCard(without, "lastrank-review") {
		t.Error("the LastRank Review card leaked to a user who cannot act on it")
	}

	// Every user keeps the unconditional card, so an empty permission set does not
	// silently produce an empty dashboard.
	if !hasCard(without, "health") {
		t.Error("the health card should be visible to everyone")
	}
}

// Admins hold every permission via allPermissionsTrue, so the card must appear for
// them without needing a rank-based special case.
func TestLastRankReviewCardVisibleToAdmin(t *testing.T) {
	if !hasCard(allowedCards(PageData{IsAdmin: true, Permissions: allPermissionsTrue()}), "lastrank-review") {
		t.Error("an admin should see the LastRank Review card")
	}
}

// The login nudge is gated by a data attribute on #layout-config, because
// script-src is 'self' only — an inline <script> carrying the flag would be
// silently blocked in production. This asserts the attribute actually renders,
// which curl cannot check without a session.
func TestLayoutRendersReviewNudgeGate(t *testing.T) {
	cases := []struct {
		name string
		perm bool
		want string
	}{
		{"manager", true, `data-can-manage-members="true"`},
		{"non-manager", false, `data-can-manage-members="false"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := template.ParseFiles("templates/layout.html", "templates/dashboard.html")
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var buf bytes.Buffer
			data := PageData{
				Title:           "Dashboard",
				IsAuthenticated: true,
				Permissions:     RankPermissions{ManageMembers: c.perm},
			}
			if err := tmpl.ExecuteTemplate(&buf, "layout.html", data); err != nil {
				t.Fatalf("execute: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, `id="layout-config"`) {
				t.Fatal("#layout-config is missing — the nudge can never fire")
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("expected %s in the rendered layout", c.want)
			}
		})
	}
}
