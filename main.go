// main.go - Entry point for the Alliance Manager application. Sets up routing, middleware, and starts the HTTP server.

package main

import (
	"context"
	"crypto/rand"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/microcosm-cc/bluemonday"
)

// getPageData loads the current user from the database and populates PageData.
// The session cookie is only used to identify the user (user_id); all
// authorization data is sourced live from the DB.
func getPageData(r *http.Request, title, activePage string) PageData {
	data := PageData{
		Title:      title,
		ActivePage: activePage,
	}

	session, _ := store.Get(r, "session")
	userID, ok := session.Values["user_id"].(int)
	if ok && userID > 0 {
		user := loadUserFromDB(userID)
		if user != nil {
			data.IsAuthenticated = true
			data.Username = user.Username
			data.Rank = user.Rank
			if user.MemberID != nil {
				data.MemberID = *user.MemberID
				db.QueryRow(
					"SELECT COALESCE(lastrank_photo_url, ''), COALESCE(lastrank_photo_failover, '') FROM members WHERE id = ?",
					*user.MemberID,
				).Scan(&data.UserPhotoURL, &data.UserPhotoFailover)
			}
			if user.IsAdmin {
				data.IsAdmin = true
				data.Permissions = allPermissionsTrue()
			} else if user.Rank != "" {
				data.Permissions = getRankPermissions(user.Rank)
			}
		}
	}

	// SPRINT 2 FIX: Inject the CSRF token into the page data so frontend forms/fetch requests can use it
	data.CSRFToken = csrf.TemplateField(r)

	// Check if GCP Vision credentials exist
	var hasGCP bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM credentials WHERE service_name = 'gcp_vision')").Scan(&hasGCP)
	data.HasGCPCredentials = hasGCP

	// Load alliance identity for the sidebar brand, plus our server number (used by the NAP tab and
	// to default the external-alliance search filter). Extends this existing query rather than
	// adding another settings round-trip.
	var allianceName, allianceTag string
	var ourServerID int
	db.QueryRow(
		"SELECT COALESCE(alliance_name, ''), COALESCE(alliance_tag, ''), COALESCE(our_server_id, 0) FROM settings WHERE id = 1",
	).Scan(&allianceName, &allianceTag, &ourServerID)
	data.AllianceName = allianceName
	data.AllianceTag = allianceTag
	data.OurServerID = ourServerID

	// NEW: Check if the CV Worker URL is configured + which OCR backend is active
	var cvWorkerURL, ocrMode string
	db.QueryRow(
		"SELECT COALESCE(cv_worker_url, ''), COALESCE(ocr_backend_mode, 'cloud') FROM settings WHERE id = 1",
	).Scan(&cvWorkerURL, &ocrMode)

	if ocrMode != string(OCRBackendLocal) {
		ocrMode = string(OCRBackendCloud)
	}
	data.OCRBackendMode = ocrMode

	// The pipeline is ready when:
	//   - cloud mode: GCP credentials AND a worker URL are configured
	//   - local mode: just a worker URL (the local sidecar URL); no GCP needed
	if ocrMode == string(OCRBackendLocal) {
		data.OCRPipelineReady = cvWorkerURL != ""
	} else {
		data.OCRPipelineReady = hasGCP && cvWorkerURL != ""
	}

	data.SkillLabels = SkillLabels

	return data
}

// renderTemplate parses the shared layout and the specific page together
func renderTemplate(w http.ResponseWriter, r *http.Request, tmplName string, data PageData) {
	t, err := template.ParseFiles("templates/layout.html", "templates/"+tmplName)
	if err != nil {
		slog.Error("Template parsing error", "error", err, "template", tmplName)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = t.ExecuteTemplate(w, "layout.html", data)
	if err != nil {
		slog.Error("Template execution error", "error", err, "template", tmplName)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func main() {
	// 1. Load Environment Variables
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, relying on system environment variables")
	}

	// 2. Set up Structured JSON Logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	slog.Info("Initializing Alliance Manager server")

	// 3. Initialize Database & Sessions
	initSessionStore()

	if err := initDB(); err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Sync the operator's OCR_BACKEND_MODE env choice into settings (replaces the
	// old racy sqlite3 write in install.sh / update.sh). Runs post-migration.
	reconcileOCRBackendFromEnv()

	// Start the local-archive retention janitor (no-op unless OCR_ARCHIVE_DIR set).
	startLocalArchiveJanitor()

	router := mux.NewRouter()

	// Auth routes (public)
	router.HandleFunc("/api/login", login).Methods("POST")
	router.HandleFunc("/api/force-change-password", forceChangePassword).Methods("POST")
	router.HandleFunc("/api/logout", logout).Methods("POST")
	router.HandleFunc("/api/change-password", authMiddleware(changePassword)).Methods("POST")
	router.HandleFunc("/api/members/{id}/invite", authMiddleware(requirePermission("manage_members", generateInvite))).Methods("POST")
	router.HandleFunc("/invite/{token}", showInvitePage).Methods("GET")
	router.HandleFunc("/invite/{token}", claimInvite).Methods("POST")

	// Activity log
	router.HandleFunc("/api/activity", authMiddleware(getActivityLog)).Methods("GET")

	// Accountability
	router.HandleFunc("/accountability", authMiddleware(handleAccountability)).Methods("GET")
	router.HandleFunc("/accountability/report", authMiddleware(handleAccountabilityReport)).Methods("GET")
	router.HandleFunc("/accountability/{id:[0-9]+}", authMiddleware(handleAccountabilityProfile)).Methods("GET")
	router.HandleFunc("/api/accountability/summary", authMiddleware(handleAccountabilitySummary)).Methods("GET")
	router.HandleFunc("/api/accountability/strikes", authMiddleware(handleAllStrikes)).Methods("GET")
	router.HandleFunc("/api/accountability/strike-types", authMiddleware(handleStrikeTypes)).Methods("GET")
	router.HandleFunc("/api/accountability/storm-attendance", authMiddleware(handleStormAttendanceForDate)).Methods("GET")
	router.HandleFunc("/api/accountability/members", authMiddleware(handleAccountabilityMembers)).Methods("GET")
	router.HandleFunc("/api/accountability/members/{id:[0-9]+}", authMiddleware(handleAccountabilityMemberProfile)).Methods("GET")
	router.HandleFunc("/api/accountability/report-data", authMiddleware(handleAccountabilityReportData)).Methods("GET")
	router.HandleFunc("/api/accountability/strikes", authMiddleware(requirePermission("manage_accountability", handleStrikeCreate))).Methods("POST")
	router.HandleFunc("/api/accountability/strikes/{id:[0-9]+}", authMiddleware(requirePermission("manage_accountability", handleStrikeUpdate))).Methods("PUT")
	router.HandleFunc("/api/accountability/strikes/{id:[0-9]+}", authMiddleware(requirePermission("manage_accountability", handleStrikeDelete))).Methods("DELETE")
	router.HandleFunc("/api/accountability/storm-attendance", authMiddleware(requirePermission("manage_accountability", handleStormAttendanceUpsert))).Methods("POST")
	router.HandleFunc("/api/train-logs/{id:[0-9]+}/showed-up", authMiddleware(requirePermission("manage_accountability", handleTrainNoShow))).Methods("PUT")

	// Season Hub routes
	router.HandleFunc("/api/season-hub/data", authMiddleware(requirePermission("view_season_hub", handleSeasonHubData))).Methods("GET")
	router.HandleFunc("/api/season-hub/seasons", authMiddleware(requirePermission("view_season_hub", handleSeasonList))).Methods("GET")
	router.HandleFunc("/api/season-hub/seasons", authMiddleware(requirePermission("manage_season_rewards", handleSeasonCreate))).Methods("POST")
	router.HandleFunc("/api/season-hub/seasons/{id:[0-9]+}/archive", authMiddleware(requirePermission("manage_season_rewards", handleSeasonArchive))).Methods("POST")
	router.HandleFunc("/api/season-hub/seasons/{id:[0-9]+}", authMiddleware(requirePermission("manage_season_rewards", handleSeasonUpdate))).Methods("PUT")
	router.HandleFunc("/api/season-hub/seasons/{id:[0-9]+}", authMiddleware(requirePermission("manage_season_rewards", handleSeasonDelete))).Methods("DELETE")
	router.HandleFunc("/api/season-hub/participation", authMiddleware(requirePermission("view_season_hub", handleParticipationGet))).Methods("GET")
	router.HandleFunc("/api/season-hub/participation", authMiddleware(requirePermission("manage_season_hub", handleParticipationSave))).Methods("PUT")
	router.HandleFunc("/api/season-hub/contributions", authMiddleware(requirePermission("manage_season_hub", handleContributionsGet))).Methods("GET")
	router.HandleFunc("/api/season-hub/contributions/import", authMiddleware(requirePermission("manage_season_hub", handleContributionsImport))).Methods("POST")
	router.HandleFunc("/api/season-hub/contributions/manual", authMiddleware(requirePermission("manage_season_hub", handleContributionsManual))).Methods("POST")
	router.HandleFunc("/api/season-hub/rewards", authMiddleware(requirePermission("view_season_hub", handleRewardsGet))).Methods("GET")
	router.HandleFunc("/api/season-hub/rewards", authMiddleware(requirePermission("manage_season_rewards", handleRewardSave))).Methods("POST")
	router.HandleFunc("/api/season-hub/rewards/{id:[0-9]+}", authMiddleware(requirePermission("manage_season_rewards", handleRewardUpdate))).Methods("PUT")
	router.HandleFunc("/api/season-hub/rewards/{id:[0-9]+}", authMiddleware(requirePermission("manage_season_rewards", handleRewardDelete))).Methods("DELETE")
	router.HandleFunc("/api/comms/templates", authMiddleware(requirePermission("view_comms", handleCommsTemplateList))).Methods("GET")
	router.HandleFunc("/api/comms/templates/slug/{slug}", authMiddleware(requirePermission("view_comms", handleCommsTemplateBySlug))).Methods("GET")
	router.HandleFunc("/api/comms/templates", authMiddleware(requirePermission("manage_comms", handleCommsTemplateCreate))).Methods("POST")
	router.HandleFunc("/api/comms/templates/{id:[0-9]+}", authMiddleware(requirePermission("manage_comms", handleCommsTemplateUpdate))).Methods("PUT")
	router.HandleFunc("/api/comms/templates/{id:[0-9]+}", authMiddleware(requirePermission("manage_comms", handleCommsTemplateDelete))).Methods("DELETE")
	router.HandleFunc("/api/comms/resources", authMiddleware(requirePermission("view_comms", handleCommsResourceList))).Methods("GET")
	router.HandleFunc("/api/comms/resources", authMiddleware(requirePermission("manage_comms", handleCommsResourceCreate))).Methods("POST")
	router.HandleFunc("/api/comms/resources/{id:[0-9]+}", authMiddleware(requirePermission("manage_comms", handleCommsResourceUpdate))).Methods("PUT")
	router.HandleFunc("/api/comms/resources/{id:[0-9]+}", authMiddleware(requirePermission("manage_comms", handleCommsResourceDelete))).Methods("DELETE")
	// Poll templates
	router.HandleFunc("/api/comms/poll-templates", authMiddleware(requirePermission("view_polls", handlePollTemplateList))).Methods("GET")
	router.HandleFunc("/api/comms/poll-templates", authMiddleware(requirePermission("manage_polls", handlePollTemplateCreate))).Methods("POST")
	router.HandleFunc("/api/comms/poll-templates/{id:[0-9]+}", authMiddleware(requirePermission("manage_polls", handlePollTemplateUpdate))).Methods("PUT")
	router.HandleFunc("/api/comms/poll-templates/{id:[0-9]+}", authMiddleware(requirePermission("manage_polls", handlePollTemplateDelete))).Methods("DELETE")
	// Poll instances
	router.HandleFunc("/api/comms/poll-instances", authMiddleware(requirePermission("view_polls", handlePollInstanceList))).Methods("GET")
	router.HandleFunc("/api/comms/poll-instances", authMiddleware(requirePermission("manage_polls", handlePollInstanceCreate))).Methods("POST")
	router.HandleFunc("/api/comms/poll-instances/{id:[0-9]+}", authMiddleware(requirePermission("manage_polls", handlePollInstanceUpdate))).Methods("PUT")
	router.HandleFunc("/api/comms/poll-instances/{id:[0-9]+}", authMiddleware(requirePermission("manage_polls", handlePollInstanceDelete))).Methods("DELETE")
	router.HandleFunc("/api/comms/poll-instances/{id:[0-9]+}/detail", authMiddleware(requirePermission("view_polls", handlePollInstanceDetail))).Methods("GET")
	// Poll responses
	router.HandleFunc("/api/comms/poll-instances/{id:[0-9]+}/responses", authMiddleware(requirePermission("manage_polls", handlePollResponseSet))).Methods("POST")
	router.HandleFunc("/api/comms/poll-instances/{id:[0-9]+}/responses/{memberID:[0-9]+}", authMiddleware(requirePermission("manage_polls", handlePollResponseClear))).Methods("DELETE")
	router.HandleFunc("/api/comms/poll-instances/{id:[0-9]+}/responses/{memberID:[0-9]+}/toggle", authMiddleware(requirePermission("manage_polls", handlePollResponseToggle))).Methods("POST")
	// Anonymous counts
	router.HandleFunc("/api/comms/poll-instances/{id:[0-9]+}/anonymous-counts", authMiddleware(requirePermission("manage_polls", handlePollAnonCountsUpdate))).Methods("PUT")
	router.HandleFunc("/api/season-hub/trackables", authMiddleware(requirePermission("view_season_hub", handleSeasonTrackableList))).Methods("GET")
	router.HandleFunc("/api/season-hub/trackables", authMiddleware(requirePermission("manage_season_hub", handleSeasonTrackableCreate))).Methods("POST")
	router.HandleFunc("/api/season-hub/trackables/{id:[0-9]+}", authMiddleware(requirePermission("manage_season_hub", handleSeasonTrackableUpdate))).Methods("PUT")
	router.HandleFunc("/api/season-hub/trackables/{id:[0-9]+}", authMiddleware(requirePermission("manage_season_hub", handleSeasonTrackableDelete))).Methods("DELETE")
	router.HandleFunc("/api/season-hub/season-events", authMiddleware(requirePermission("view_season_hub", handleSeasonEventList))).Methods("GET")
	router.HandleFunc("/api/season-hub/season-events/push", authMiddleware(requirePermission("manage_season_hub", handleSeasonEventPushToSchedule))).Methods("POST")
	router.HandleFunc("/api/season-hub/season-events", authMiddleware(requirePermission("manage_season_hub", handleSeasonEventCreate))).Methods("POST")
	router.HandleFunc("/api/season-hub/season-events/{id:[0-9]+}", authMiddleware(requirePermission("manage_season_hub", handleSeasonEventUpdate))).Methods("PUT")
	router.HandleFunc("/api/season-hub/season-events/{id:[0-9]+}", authMiddleware(requirePermission("manage_season_hub", handleSeasonEventDelete))).Methods("DELETE")
	router.HandleFunc("/api/season-hub/templates", authMiddleware(requirePermission("view_season_hub", handleSeasonTemplateList))).Methods("GET")
	router.HandleFunc("/api/season-hub/templates", authMiddleware(requirePermission("manage_settings", handleSeasonTemplateCreate))).Methods("POST")
	router.HandleFunc("/api/season-hub/templates/{id:[0-9]+}", authMiddleware(requirePermission("view_season_hub", handleSeasonTemplateGet))).Methods("GET")
	router.HandleFunc("/api/season-hub/templates/{id:[0-9]+}", authMiddleware(requirePermission("manage_settings", handleSeasonTemplateUpdate))).Methods("PUT")
	router.HandleFunc("/api/season-hub/templates/{id:[0-9]+}", authMiddleware(requirePermission("manage_settings", handleSeasonTemplateDelete))).Methods("DELETE")
	router.HandleFunc("/api/season-hub/templates/{id:[0-9]+}/sync-event-types", authMiddleware(requirePermission("manage_settings", handleSeasonTemplateSyncEventTypes))).Methods("POST")
	router.HandleFunc("/api/season-hub/score-levels-default", authMiddleware(requirePermission("view_season_hub", handleSeasonScoreLevelsDefaultGet))).Methods("GET")
	router.HandleFunc("/api/season-hub/score-levels-default", authMiddleware(requirePermission("manage_settings", handleSeasonScoreLevelsDefaultPut))).Methods("PUT")

	// Admin routes (admin only)
	router.HandleFunc("/api/admin/users", authMiddleware(adminMiddleware(getAdminUsers))).Methods("GET")
	router.HandleFunc("/api/admin/users", authMiddleware(adminMiddleware(createAdminUser))).Methods("POST")
	router.HandleFunc("/api/admin/users/{id}", authMiddleware(adminMiddleware(updateAdminUser))).Methods("PUT")
	router.HandleFunc("/api/admin/users/{id}", authMiddleware(adminMiddleware(deleteAdminUser))).Methods("DELETE")
	router.HandleFunc("/api/admin/users/{id}/reset-password", authMiddleware(adminMiddleware(resetUserPassword))).Methods("POST")
	router.HandleFunc("/api/admin/login-history", authMiddleware(adminMiddleware(getLoginHistory))).Methods("GET")
	router.HandleFunc("/api/admin/users/{id}/file-count", authMiddleware(adminMiddleware(getUserFileCount))).Methods("GET")
	router.HandleFunc("/api/admin/users/{id}/transfer-files", authMiddleware(adminMiddleware(transferUserFiles))).Methods("POST")

	// Add these to the Admin Routes section in main.go
	router.HandleFunc("/api/admin/security/password-policy", authMiddleware(adminMiddleware(updatePasswordPolicy))).Methods("PUT")
	router.HandleFunc("/api/admin/security/cv-worker", authMiddleware(adminMiddleware(updateCVWorkerURL))).Methods("PUT")
	router.HandleFunc("/api/admin/security/ocr-archive", authMiddleware(adminMiddleware(updateOCRArchiveSettings))).Methods("PUT")
	router.HandleFunc("/api/admin/credentials", authMiddleware(adminMiddleware(updateExternalCredentials))).Methods("POST")
	router.HandleFunc("/api/admin/credentials/{service}", authMiddleware(adminMiddleware(deleteExternalCredential))).Methods("DELETE")

	// Files Implementation API
	router.HandleFunc("/api/files", authMiddleware(requirePermission("view_files", getFilesList))).Methods("GET")
	router.HandleFunc("/api/files/upload", authMiddleware(requirePermission("upload_files", uploadFile))).Methods("POST")
	router.HandleFunc("/api/files/{id}", authMiddleware(updateFile)).Methods("PUT")
	router.HandleFunc("/api/files/{id}", authMiddleware(deleteFile)).Methods("DELETE")
	router.HandleFunc("/api/files/download/{id}", authMiddleware(downloadFile)).Methods("GET")
	router.HandleFunc("/api/files/{id}/wopi-token", authMiddleware(generateWOPIToken)).Methods("GET")

	// WOPI Collabora API
	wopiRouter := router.PathPrefix("/wopi").Subrouter()
	wopiRouter.HandleFunc("/files/{id}", wopiAuthMiddleware(wopiCheckFileInfo)).Methods("GET")
	wopiRouter.HandleFunc("/files/{id}/contents", wopiAuthMiddleware(wopiGetFile)).Methods("GET")

	// WOPI POST routes (CSRF exemption is handled at the server level below)
	wopiRouter.HandleFunc("/files/{id}", wopiAuthMiddleware(wopiActionHandler)).Methods("POST")
	wopiRouter.HandleFunc("/files/{id}/contents", wopiAuthMiddleware(wopiPutFile)).Methods("POST")

	// Schedule event types
	router.HandleFunc("/api/schedule/event-types", authMiddleware(requirePermission("view_schedule", getScheduleEventTypes))).Methods("GET")
	router.HandleFunc("/api/schedule/event-types", authMiddleware(requirePermission("manage_schedule", createScheduleEventType))).Methods("POST")
	router.HandleFunc("/api/schedule/event-types/{id:[0-9]+}", authMiddleware(requirePermission("manage_schedule", updateScheduleEventType))).Methods("PUT")
	router.HandleFunc("/api/schedule/event-types/{id:[0-9]+}", authMiddleware(requirePermission("manage_schedule", deleteScheduleEventType))).Methods("DELETE")

	// Calendar events
	router.HandleFunc("/api/schedule/events", authMiddleware(requirePermission("view_schedule", getScheduleEvents))).Methods("GET")
	router.HandleFunc("/api/schedule/events", authMiddleware(requirePermission("manage_schedule", createScheduleEvent))).Methods("POST")
	router.HandleFunc("/api/schedule/events/{id:[0-9]+}", authMiddleware(requirePermission("manage_schedule", updateScheduleEvent))).Methods("PUT")
	router.HandleFunc("/api/schedule/events/{id:[0-9]+}", authMiddleware(requirePermission("manage_schedule", deleteScheduleEvent))).Methods("DELETE")

	// Event generation (must be registered before the bare /events route; gorilla/mux matches most specific first)
	router.HandleFunc("/api/schedule/events/generate", authMiddleware(requirePermission("manage_schedule", generateScheduleEvents))).Methods("POST")

	// Server events
	router.HandleFunc("/api/schedule/server-events", authMiddleware(requirePermission("view_schedule", getServerEvents))).Methods("GET")
	router.HandleFunc("/api/schedule/server-events", authMiddleware(requirePermission("manage_schedule", createServerEvent))).Methods("POST")
	router.HandleFunc("/api/schedule/server-events/{id:[0-9]+}", authMiddleware(requirePermission("manage_schedule", updateServerEvent))).Methods("PUT")
	router.HandleFunc("/api/schedule/server-events/{id:[0-9]+}", authMiddleware(requirePermission("manage_schedule", deleteServerEvent))).Methods("DELETE")

	// Storm slot times (read: all authenticated; write: admin only)
	router.HandleFunc("/api/storm/slot-times", authMiddleware(getAdvancedStormSlots)).Methods("GET")
	router.HandleFunc("/api/admin/advanced/storm-slots", authMiddleware(adminMiddleware(putAdvancedStormSlots))).Methods("PUT")

	// Train Tracker API
	router.HandleFunc("/api/train-logs", authMiddleware(requirePermission("view_train", getTrainLogs))).Methods("GET")
	router.HandleFunc("/api/train-logs", authMiddleware(requirePermission("manage_train", postTrainLog))).Methods("POST")
	router.HandleFunc("/api/train-logs/{id:[0-9]+}", authMiddleware(requirePermission("manage_train", putTrainLog))).Methods("PUT")
	router.HandleFunc("/api/train-logs/{id:[0-9]+}", authMiddleware(requirePermission("manage_train", deleteTrainLog))).Methods("DELETE")
	router.HandleFunc("/api/eligibility-rules", authMiddleware(requirePermission("view_train", getEligibilityRules))).Methods("GET")
	router.HandleFunc("/api/eligibility-rules", authMiddleware(requirePermission("manage_train", postEligibilityRule))).Methods("POST")
	router.HandleFunc("/api/eligibility-rules/{id:[0-9]+}", authMiddleware(requirePermission("manage_train", putEligibilityRule))).Methods("PUT")
	router.HandleFunc("/api/eligibility-rules/{id:[0-9]+}", authMiddleware(requirePermission("manage_train", deleteEligibilityRule))).Methods("DELETE")
	router.HandleFunc("/api/eligibility-rules/{id:[0-9]+}/run", authMiddleware(requirePermission("manage_train", runEligibilityRule))).Methods("POST")

	// Permission Matrix & Settings routes
	router.HandleFunc("/api/permissions", authMiddleware(requirePermission("manage_settings", getPermissionsMatrix))).Methods("GET")
	router.HandleFunc("/api/permissions", authMiddleware(requirePermission("manage_settings", updatePermissionsMatrix))).Methods("PUT")
	router.HandleFunc("/api/permissions/schema", authMiddleware(requirePermission("manage_settings", getPermissionsSchema))).Methods("GET")
	router.HandleFunc("/api/settings", authMiddleware(getSettings)).Methods("GET")
	router.HandleFunc("/api/settings", authMiddleware(requirePermission("manage_settings", updateSettings))).Methods("PUT")

	// Members API
	router.HandleFunc("/api/members", authMiddleware(getMembers)).Methods("GET")
	router.HandleFunc("/api/members/stats", authMiddleware(getMemberStats)).Methods("GET")
	router.HandleFunc("/api/members", authMiddleware(requirePermission("manage_members", createMember))).Methods("POST")
	router.HandleFunc("/api/members/{id:[0-9]+}", authMiddleware(requirePermission("manage_members", updateMember))).Methods("PUT")
	router.HandleFunc("/api/members/{id:[0-9]+}", authMiddleware(adminMiddleware(deleteMember))).Methods("DELETE")
	router.HandleFunc("/api/members/{id:[0-9]+}/archive", authMiddleware(requirePermission("manage_members", archiveMember))).Methods("PUT")
	router.HandleFunc("/api/members/{id:[0-9]+}/reactivate", authMiddleware(requirePermission("manage_members", reactivateMember))).Methods("PUT")
	router.HandleFunc("/api/former-members", authMiddleware(requirePermission("manage_members", getFormerMembers))).Methods("GET")
	router.HandleFunc("/api/former-members/{id:[0-9]+}", authMiddleware(requirePermission("manage_members", updateFormerMember))).Methods("PUT")
	router.HandleFunc("/api/members/import", authMiddleware(requirePermission("manage_members", importCSV))).Methods("POST")
	router.HandleFunc("/api/members/import/confirm", authMiddleware(requirePermission("manage_members", confirmMemberUpdates))).Methods("POST")
	router.HandleFunc("/api/skills", authMiddleware(getSkillRegistry)).Methods("GET")
	router.HandleFunc("/api/members/skills", authMiddleware(getMembersWithSkills)).Methods("GET")
	router.HandleFunc("/api/members/{id:[0-9]+}/skills", authMiddleware(requirePermission("manage_members", updateMemberSkills))).Methods("PUT")
	router.HandleFunc("/api/profile/me/skills", authMiddleware(updateProfileSkills)).Methods("PUT")

	// Prospects API
	router.HandleFunc("/api/prospects", authMiddleware(requirePermission("view_recruiting", getProspects))).Methods("GET")
	router.HandleFunc("/api/prospects", authMiddleware(requirePermission("manage_recruiting", createProspect))).Methods("POST")
	router.HandleFunc("/api/prospects/{id:[0-9]+}", authMiddleware(requirePermission("manage_recruiting", updateProspect))).Methods("PUT")
	router.HandleFunc("/api/prospects/{id:[0-9]+}", authMiddleware(requirePermission("manage_recruiting", deleteProspect))).Methods("DELETE")
	router.HandleFunc("/api/prospects/{id:[0-9]+}/convert", authMiddleware(requirePermission("manage_members", convertProspectToMember))).Methods("POST")

	// Allies API
	router.HandleFunc("/api/ally-agreement-types", authMiddleware(getAllyAgreementTypes)).Methods("GET")
	router.HandleFunc("/api/ally-agreement-types", authMiddleware(requirePermission("manage_allies", createAllyAgreementType))).Methods("POST")
	router.HandleFunc("/api/ally-agreement-types/{id:[0-9]+}", authMiddleware(requirePermission("manage_allies", updateAllyAgreementType))).Methods("PUT")
	router.HandleFunc("/api/ally-agreement-types/{id:[0-9]+}", authMiddleware(requirePermission("manage_allies", deleteAllyAgreementType))).Methods("DELETE")
	// NAP tab. The GET is view-gated (matching the page gate and getExternalAlliancesGated); the
	// refresh is manage-gated because it hits the volunteer-run LastRank service.
	router.HandleFunc("/api/allies/nap", authMiddleware(requirePermission("view_allies", getNAP))).Methods("GET")
	// Refresh is a three-phase, browser-driven flow (ladder → member count per alliance → finish),
	// mirroring the LastRank member sync. See handlers_nap.go.
	router.HandleFunc("/api/allies/nap/refresh", authMiddleware(requirePermission("manage_allies", refreshNAP))).Methods("POST")
	router.HandleFunc("/api/allies/nap/member", authMiddleware(requirePermission("manage_allies", napMemberCount))).Methods("POST")
	router.HandleFunc("/api/allies/nap/finish", authMiddleware(requirePermission("manage_allies", napFinish))).Methods("POST")
	router.HandleFunc("/api/allies", authMiddleware(getAllies)).Methods("GET")
	router.HandleFunc("/api/allies", authMiddleware(requirePermission("manage_allies", createAlly))).Methods("POST")
	router.HandleFunc("/api/allies/{id:[0-9]+}", authMiddleware(requirePermission("manage_allies", updateAlly))).Methods("PUT")
	router.HandleFunc("/api/allies/{id:[0-9]+}", authMiddleware(requirePermission("manage_allies", deleteAlly))).Methods("DELETE")
	router.HandleFunc("/api/profile/me", authMiddleware(getMyProfile)).Methods("GET")
	router.HandleFunc("/api/profile/me", authMiddleware(updateMyProfile)).Methods("PUT")

	// Member Aliases API
	router.HandleFunc("/api/members/{id:[0-9]+}/aliases", authMiddleware(getMemberAliases)).Methods("GET")
	router.HandleFunc("/api/members/{id:[0-9]+}/aliases", authMiddleware(addMemberAlias)).Methods("POST")
	router.HandleFunc("/api/aliases/{id:[0-9]+}", authMiddleware(deleteMemberAlias)).Methods("DELETE")

	// VS points
	router.HandleFunc("/api/vs-points", authMiddleware(requirePermission("view_vs_points", getVSPoints))).Methods("GET")
	router.HandleFunc("/api/vs-points", authMiddleware(requirePermission("manage_vs_points", saveVSPoints))).Methods("POST")
	router.HandleFunc("/api/vs-points/{week}", authMiddleware(requirePermission("manage_vs_points", deleteWeekVSPoints))).Methods("DELETE")

	router.HandleFunc("/api/vs-points/import/preview", authMiddleware(requirePermission("manage_members", previewCSVImport))).Methods("POST")
	router.HandleFunc("/api/vs-points/import/commit", authMiddleware(requirePermission("manage_members", commitCSVImport))).Methods("POST")

	// VS Duel League — read = view_vs_points, write = manage_vs_points
	router.HandleFunc("/api/vs-league/current", authMiddleware(requirePermission("view_vs_points", getVSLeagueCurrent))).Methods("GET")
	router.HandleFunc("/api/vs-league/seasons", authMiddleware(requirePermission("view_vs_points", getVSLeagueSeasons))).Methods("GET")
	router.HandleFunc("/api/vs-league/seasons", authMiddleware(requirePermission("manage_vs_points", createVSLeagueSeason))).Methods("POST")
	router.HandleFunc("/api/vs-league/seasons/{id:[0-9]+}", authMiddleware(requirePermission("manage_vs_points", updateVSLeagueSeason))).Methods("PUT")
	router.HandleFunc("/api/vs-league/weeks", authMiddleware(requirePermission("view_vs_points", getVSLeagueWeeks))).Methods("GET")
	router.HandleFunc("/api/vs-league/weeks", authMiddleware(requirePermission("manage_vs_points", createVSLeagueWeek))).Methods("POST")
	router.HandleFunc("/api/vs-league/weeks/{id:[0-9]+}", authMiddleware(requirePermission("manage_vs_points", updateVSLeagueWeek))).Methods("PUT")
	router.HandleFunc("/api/vs-league/weeks/{id:[0-9]+}", authMiddleware(requirePermission("manage_vs_points", deleteVSLeagueWeek))).Methods("DELETE")
	router.HandleFunc("/api/vs-league/weeks/{id:[0-9]+}/days", authMiddleware(requirePermission("manage_vs_points", saveVSLeagueDays))).Methods("POST")
	router.HandleFunc("/api/vs-league/weeks/{id:[0-9]+}/matchups", authMiddleware(requirePermission("view_vs_points", getVSLeagueMatchups))).Methods("GET")
	router.HandleFunc("/api/vs-league/weeks/{id:[0-9]+}/matchups", authMiddleware(requirePermission("manage_vs_points", saveVSLeagueMatchups))).Methods("POST")
	router.HandleFunc("/api/vs-league/participation", authMiddleware(requirePermission("view_vs_points", getVSLeagueParticipation))).Methods("GET")
	router.HandleFunc("/api/vs-league/analytics", authMiddleware(requirePermission("view_vs_points", getVSLeagueAnalytics))).Methods("GET")
	router.HandleFunc("/api/vs-league/opponent-lookup", authMiddleware(requirePermission("manage_vs_points", vsLeagueOpponentLookup))).Methods("POST")
	router.HandleFunc("/api/vs-league/opponent-roster", authMiddleware(requirePermission("manage_vs_points", vsLeagueOpponentRoster))).Methods("GET")
	router.HandleFunc("/api/vs-league/our-snapshot", authMiddleware(requirePermission("manage_vs_points", vsLeagueOurSnapshot))).Methods("GET")
	router.HandleFunc("/api/external-alliances", authMiddleware(getExternalAlliancesGated)).Methods("GET")
	router.HandleFunc("/api/external-alliances", authMiddleware(requireManageExternalAlliances(createExternalAlliance))).Methods("POST")
	router.HandleFunc("/api/external-alliances/{id:[0-9]+}", authMiddleware(requireManageExternalAlliances(updateExternalAlliance))).Methods("PUT")
	router.HandleFunc("/api/external-alliances/{id:[0-9]+}", authMiddleware(requireManageExternalAlliances(deleteExternalAlliance))).Methods("DELETE")
	router.HandleFunc("/api/external-alliances/{id:[0-9]+}/refresh", authMiddleware(requireManageExternalAlliances(refreshExternalAlliance))).Methods("POST")
	router.HandleFunc("/api/external-alliances/lookup", authMiddleware(requireManageExternalAlliances(lookupExternalAlliance))).Methods("POST")
	router.HandleFunc("/api/external-alliances/search", authMiddleware(requireManageExternalAlliances(searchExternalAlliancesLastRank))).Methods("GET")

	// LastRank.fun enrichment (manual trigger; client-side rate-limited globally)
	router.HandleFunc("/api/lastrank/preview", authMiddleware(requirePermission("manage_members", lastRankPreview))).Methods("POST")
	router.HandleFunc("/api/lastrank/commit", authMiddleware(requirePermission("manage_members", lastRankCommit))).Methods("POST")
	router.HandleFunc("/api/lastrank/player", authMiddleware(requirePermission("manage_members", lastRankSyncPlayer))).Methods("POST")
	router.HandleFunc("/api/lastrank/finish", authMiddleware(requirePermission("manage_members", lastRankFinish))).Methods("POST")
	router.HandleFunc("/api/lastrank/prospect", authMiddleware(requirePermission("manage_recruiting", lastRankProspectLookup))).Methods("POST")
	// Same finish handler, gated for recruiting officers (kind="prospects").
	router.HandleFunc("/api/lastrank/prospect/finish", authMiddleware(requirePermission("manage_recruiting", lastRankFinish))).Methods("POST")

	// Mobile API (bearer token auth, CSRF exempt)
	router.HandleFunc("/api/mobile/login", mobileLogin).Methods("POST")
	router.HandleFunc("/api/mobile/members", mobileBearerMiddleware(getMobileMembers)).Methods("GET")
	router.HandleFunc("/api/mobile/preview", mobileBearerMiddleware(requireMobilePermission("manage_vs", mobilePreview))).Methods("POST")
	router.HandleFunc("/api/mobile/commit", mobileBearerMiddleware(requireMobilePermission("manage_vs", mobileCommit))).Methods("POST")

	// Dyno
	router.HandleFunc("/api/dyno-recommendations", authMiddleware(getDynoRecommendations)).Methods("GET")
	router.HandleFunc("/api/dyno-recommendations", authMiddleware(createDynoRecommendation)).Methods("POST")
	router.HandleFunc("/api/dyno-recommendations/{id}", authMiddleware(deleteDynoRecommendation)).Methods("DELETE")
	router.HandleFunc("/api/dyno-recommendations/{id:[0-9]+}", authMiddleware(updateDynoRecommendation)).Methods("PUT")

	// Rankings & Timelines
	router.HandleFunc("/api/rankings", authMiddleware(getMemberRankings)).Methods("GET")
	router.HandleFunc("/api/member-timelines", authMiddleware(getMemberTimelines)).Methods("GET")

	// Storm & Power History
	router.HandleFunc("/api/storm-assignments", authMiddleware(getStormAssignments)).Methods("GET")
	router.HandleFunc("/api/storm-assignments", authMiddleware(requirePermission("manage_storm", saveStormAssignments))).Methods("POST")
	router.HandleFunc("/api/storm-assignments/{taskForce}", authMiddleware(requirePermission("manage_storm", deleteStormAssignments))).Methods("DELETE")

	// Storm TF config
	router.HandleFunc("/api/storm/config",
		authMiddleware(getStormConfig)).Methods("GET")
	router.HandleFunc("/api/storm/config",
		authMiddleware(requirePermission("manage_storm", saveStormConfig))).Methods("PUT")

	// Storm registrations
	router.HandleFunc("/api/storm/registrations",
		authMiddleware(requirePermission("manage_storm", getStormRegistrations))).Methods("GET")
	router.HandleFunc("/api/storm/registrations/me",
		authMiddleware(getMyRegistration)).Methods("GET")
	router.HandleFunc("/api/storm/registrations/me",
		authMiddleware(upsertMyRegistration)).Methods("PUT")
	router.HandleFunc("/api/storm/registrations/{member_id:[0-9]+}",
		authMiddleware(requirePermission("manage_storm", upsertMemberRegistration))).Methods("PUT")
	router.HandleFunc("/api/storm/registrations/{member_id:[0-9]+}",
		authMiddleware(requirePermission("manage_storm", deleteMemberRegistration))).Methods("DELETE")

	// Storm groups
	router.HandleFunc("/api/storm/groups",
		authMiddleware(requirePermission("view_storm", getStormGroups))).Methods("GET")
	router.HandleFunc("/api/storm/groups",
		authMiddleware(requirePermission("manage_storm", createStormGroup))).Methods("POST")
	router.HandleFunc("/api/storm/groups/{id:[0-9]+}",
		authMiddleware(requirePermission("manage_storm", updateStormGroup))).Methods("PUT")
	router.HandleFunc("/api/storm/groups/{id:[0-9]+}",
		authMiddleware(requirePermission("manage_storm", deleteStormGroup))).Methods("DELETE")
	router.HandleFunc("/api/storm/groups/{id:[0-9]+}/buildings",
		authMiddleware(requirePermission("manage_storm", saveGroupBuildings))).Methods("PUT")
	router.HandleFunc("/api/storm/groups/{id:[0-9]+}/members",
		authMiddleware(requirePermission("manage_storm", saveGroupDirectMembers))).Methods("PUT")

	// Officer Command
	router.HandleFunc("/api/officer-command/data", authMiddleware(requirePermission("view_officer_command", getOfficerCommandData))).Methods("GET")
	router.HandleFunc("/api/officer-command/categories", authMiddleware(requirePermission("manage_officer_command", createOCCategory))).Methods("POST")
	router.HandleFunc("/api/officer-command/categories/reorder", authMiddleware(requirePermission("manage_officer_command", reorderOCCategories))).Methods("PUT")
	router.HandleFunc("/api/officer-command/categories/{id:[0-9]+}", authMiddleware(requirePermission("manage_officer_command", updateOCCategory))).Methods("PUT")
	router.HandleFunc("/api/officer-command/categories/{id:[0-9]+}", authMiddleware(requirePermission("manage_officer_command", deleteOCCategory))).Methods("DELETE")
	router.HandleFunc("/api/officer-command/responsibilities", authMiddleware(requirePermission("manage_officer_command", createOCResponsibility))).Methods("POST")
	router.HandleFunc("/api/officer-command/responsibilities/reorder", authMiddleware(requirePermission("manage_officer_command", reorderOCResponsibilities))).Methods("PUT")
	router.HandleFunc("/api/officer-command/responsibilities/{id:[0-9]+}", authMiddleware(requirePermission("manage_officer_command", updateOCResponsibility))).Methods("PUT")
	router.HandleFunc("/api/officer-command/responsibilities/{id:[0-9]+}", authMiddleware(requirePermission("manage_officer_command", deleteOCResponsibility))).Methods("DELETE")
	router.HandleFunc("/api/officer-command/responsibilities/{id:[0-9]+}/assignees", authMiddleware(requirePermission("manage_officer_command", addOCAssignee))).Methods("POST")
	router.HandleFunc("/api/officer-command/responsibilities/{id:[0-9]+}/assignees/{member_id:[0-9]+}", authMiddleware(requirePermission("manage_officer_command", removeOCAssignee))).Methods("DELETE")

	router.HandleFunc("/api/power-history", authMiddleware(getPowerHistory)).Methods("GET")
	router.HandleFunc("/api/power-history", authMiddleware(requirePermission("manage_members", addPowerRecord))).Methods("POST")

	router.HandleFunc("/api/hero-power-history", authMiddleware(getHeroPowerHistory)).Methods("GET")
	router.HandleFunc("/api/hero-power-history", authMiddleware(requirePermission("manage_members", addHeroPowerRecord))).Methods("POST")
	router.HandleFunc("/api/kill-history", authMiddleware(getKillHistory)).Methods("GET")
	router.HandleFunc("/api/kill-history", authMiddleware(requirePermission("manage_members", postKillHistory))).Methods("POST")
	// HQ level + profession level are history-only, current derived from the latest
	// row (like kills). Manual writes go through the member edit modal → updateMember,
	// so these are read-only Tracking-page feeds; no POST endpoint.
	router.HandleFunc("/api/hq-level-history", authMiddleware(getHQLevelHistory)).Methods("GET")
	router.HandleFunc("/api/profession-level-history", authMiddleware(getProfessionLevelHistory)).Methods("GET")

	router.HandleFunc("/api/smart-screenshot", authMiddleware(processSmartScreenshot)).Methods("POST")

	// --- UI Routes ---

	// 1. Custom 404 Handler
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := getPageData(r, "404 Not Found - Alliance Manager", "404")
		w.WriteHeader(http.StatusNotFound)
		renderTemplate(w, r, "404.html", data)
	})

	router.HandleFunc("/", dashboardHandler).Methods("GET")

	// Dashboard prefs API
	router.HandleFunc("/api/dashboard/prefs", authMiddleware(getDashboardPrefs)).Methods("GET")
	router.HandleFunc("/api/dashboard/prefs", authMiddleware(saveDashboardPrefs)).Methods("POST")

	// Custom Login Route (No Layout)
	router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		session, _ := store.Get(r, "session")
		if userID, ok := session.Values["user_id"].(int); ok && userID > 0 {
			http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
			return
		}

		var rawMessage string
		err := db.QueryRow("SELECT COALESCE(login_message, '') FROM settings WHERE id = 1").Scan(&rawMessage)
		if err != nil {
			rawMessage = ""
		}

		p := bluemonday.UGCPolicy()
		safeMessage := p.Sanitize(rawMessage)

		data := struct {
			LoginMessage template.HTML
			CSRFToken    template.HTML
		}{
			LoginMessage: template.HTML(safeMessage),
			CSRFToken:    csrf.TemplateField(r),
		}

		t, err := template.ParseFiles("templates/login.html")
		if err != nil {
			slog.Error("Template parsing error", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		t.Execute(w, data)
	}).Methods("GET")

	// 2. Updated Page Map (Removed Train, Awards, Recs)
	pages := map[string]string{
		"/members":         "members",
		"/dyno":            "dyno",
		"/rankings":        "rankings",
		"/storm":           "storm",
		"/vs":              "vs",
		"/upload":          "upload",
		"/settings":        "settings",
		"/admin":           "admin",
		"/profile":         "profile",
		"/files":           "files",
		"/schedule":        "schedule",
		"/officer-command": "officer-command",
		"/train":           "train",
		"/recruiting":      "recruiting",
		"/allies":          "allies",
		"/external-alliances": "external-alliances",
		"/activity":        "activity",
		"/season-hub":      "season-hub",
		"/comms":           "comms",
	}

	for path, templateName := range pages {
		p := path
		tmpl := templateName
		router.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			data := getPageData(r, strings.Title(tmpl)+" - Alliance Manager", tmpl)

			if !data.IsAuthenticated {
				http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
				return
			}

			pagePermissions := map[string]bool{
				"dyno":            data.Permissions.ViewDyno,
				"rankings":        data.Permissions.ViewRankings,
				"storm":           data.Permissions.ViewStorm,
				"vs":              data.Permissions.ViewVSPoints,
				"upload":          data.Permissions.ViewUpload,
				"settings":        data.Permissions.ManageSettings,
				"admin":           data.IsAdmin,
				"schedule":        data.Permissions.ViewSchedule,
				"officer-command": data.Permissions.ViewOfficerCommand,
				"train":           data.Permissions.ViewTrain,
				"recruiting":      data.Permissions.ViewRecruiting,
				"allies":          data.Permissions.ViewAllies,
				"external-alliances": data.Permissions.ViewAllies,
				"activity":        data.Permissions.ViewActivity || data.IsAdmin,
				"accountability":  data.Permissions.ViewAccountability,
				"season-hub":      data.Permissions.ViewSeasonHub,
				"files":           data.Permissions.ViewFiles,
				"comms":           data.Permissions.ViewComms || data.Permissions.ViewPolls,
			}

			// 3. Custom 403 Handler for Access Denied
			if hasAccess, exists := pagePermissions[tmpl]; exists && !hasAccess {
				data.Title = "403 Access Denied"
				data.ActivePage = "403"
				w.WriteHeader(http.StatusForbidden)
				renderTemplate(w, r, "403.html", data)
				return
			}

			renderTemplate(w, r, tmpl+".html", data)
		}).Methods("GET")

		router.HandleFunc(p+".html", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, p, http.StatusMovedPermanently)
		}).Methods("GET")
	}

	// --- Serve Static Files (with Custom 404 Catch) ---
	router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean the path to prevent directory traversal attacks
		cleanPath := filepath.Clean(r.URL.Path)
		fullPath := filepath.Join("static", cleanPath)

		// Check if the file actually exists in the /static folder
		info, err := os.Stat(fullPath)
		if os.IsNotExist(err) || info.IsDir() {
			// It's not a real file, so trigger our custom 404 template!
			router.NotFoundHandler.ServeHTTP(w, r)
			return
		}

		// The file exists (like style.css or app.js), serve it normally.
		// In the test/dev environment (PRODUCTION != "true") disable browser
		// caching so edited JS/CSS/SVG always reload — avoids stale assets,
		// especially on mobile. Production is unaffected.
		if os.Getenv("PRODUCTION") != "true" {
			w.Header().Set("Cache-Control", "no-store, must-revalidate")
		}
		http.FileServer(http.Dir("static")).ServeHTTP(w, r)
	})

	// 4. Initialize CSRF Protection
	var csrfKey []byte
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		// TODO: refuse to start if PRODUCTION=true and SESSION_KEY is unset or < 32 bytes.
		// Today an unset key boots with an ephemeral one (below), logging everyone out on restart.
		// Dev mode: generate an ephemeral random key (sessions won't persist across restarts)
		csrfKey = make([]byte, 32)
		if _, err := rand.Read(csrfKey); err != nil {
			slog.Error("Failed to generate ephemeral CSRF key", "error", err)
			os.Exit(1)
		}
		slog.Warn("SESSION_KEY not set; using ephemeral CSRF key")
	} else if len(sessionKey) < MinSessionKeyLen {
		slog.Error("SESSION_KEY must be at least 32 characters", "length", len(sessionKey))
		os.Exit(1)
	} else {
		csrfKey = []byte(sessionKey[:32])
	}

	// Base CSRF options
	csrfOpts := []csrf.Option{
		csrf.Secure(os.Getenv("PRODUCTION") == "true"),
		csrf.Path("/"),
	}

	// Add trusted origins for local testing and reverse proxies
	// Note: gorilla/csrf expects domains/IPs without the scheme (http://) or port (:8080)
	if trusted := os.Getenv("TRUSTED_ORIGINS"); trusted != "" {
		origins := strings.Split(trusted, ",")
		for i, o := range origins {
			origins[i] = strings.TrimSpace(o)
		}
		csrfOpts = append(csrfOpts, csrf.TrustedOrigins(origins))
		slog.Info("Added trusted origins for CSRF", "origins", origins)
	}

	csrfMiddleware := csrf.Protect(csrfKey, csrfOpts...)

	// Create the protected router handler
	protectedHandler := csrfMiddleware(router)

	// Create a conditional handler to bypass CSRF for WOPI webhooks
	appHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wopi/") ||
			strings.HasPrefix(r.URL.Path, "/api/mobile/") {
			// Send WOPI and mobile API traffic directly to the router (bypassing CSRF)
			router.ServeHTTP(w, r)
		} else {
			// Send all other traffic through the CSRF middleware
			protectedHandler.ServeHTTP(w, r)
		}
	})

	// 5. Start Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{Addr: ":" + port, Handler: appHandler}

	go func() {
		slog.Info("Server listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	// Drain in-flight OCR archive goroutines. archiveSem is acquire-by-send /
	// release-by-receive (cap 4): sending cap times blocks until every active
	// goroutine has released. BOUNDED — a hung cloud upload must not wedge exit.
	drained := make(chan struct{})
	go func() {
		for i := 0; i < cap(archiveSem); i++ {
			archiveSem <- struct{}{}
		}
		close(drained)
	}()
	select {
	case <-drained:
		slog.Info("Archive goroutines drained")
	case <-time.After(10 * time.Second):
		slog.Warn("Archive drain timed out; exiting with in-flight archives")
	}
	slog.Info("Server stopped")
}
