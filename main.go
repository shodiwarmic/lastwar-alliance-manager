// main.go - Entry point for the Alliance Manager application. Sets up routing, middleware, and starts the HTTP server.

package main

import (
	"crypto/rand"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/microcosm-cc/bluemonday"
)

// getPageData extracts the current user's session data and permissions
func getPageData(r *http.Request, title, activePage string) PageData {
	data := PageData{
		Title:      title,
		ActivePage: activePage,
	}

	session, _ := store.Get(r, "session")
	if auth, ok := session.Values["authenticated"].(bool); ok && auth {
		data.IsAuthenticated = true
		data.Username = session.Values["username"].(string)

		if adminVal, ok := session.Values["is_admin"].(bool); ok && adminVal {
			data.IsAdmin = true
			data.Permissions = RankPermissions{ViewTrain: true, ManageTrain: true, ViewAwards: true, ManageAwards: true, ViewRecs: true, ManageRecs: true, ViewDyno: true, ManageDyno: true, ViewRankings: true, ViewStorm: true, ManageStorm: true, ViewVSPoints: true, ManageVSPoints: true, ViewUpload: true, ManageMembers: true, ManageSettings: true, ViewFiles: true, ManageFiles: true, UploadFiles: true, ViewSchedule: true, ManageSchedule: true, ViewOfficerCommand: true, ManageOfficerCommand: true, ViewRecruiting: true, ManageRecruiting: true, ViewAllies: true, ManageAllies: true, ViewActivity: true, ViewAccountability: true, ManageAccountability: true}
		} else if memberID, ok := session.Values["member_id"].(int); ok {
			var rank string
			if err := db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank); err == nil {
				data.Rank = rank
				data.Permissions = getRankPermissions(rank)
			}
		}
	}

	// SPRINT 2 FIX: Inject the CSRF token into the page data so frontend forms/fetch requests can use it
	data.CSRFToken = csrf.TemplateField(r)

	// Check if GCP Vision credentials exist
	var hasGCP bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM credentials WHERE service_name = 'gcp_vision')").Scan(&hasGCP)
	data.HasGCPCredentials = hasGCP

	// NEW: Check if the CV Worker URL is configured
	var cvWorkerURL string
	db.QueryRow("SELECT COALESCE(cv_worker_url, '') FROM settings WHERE id = 1").Scan(&cvWorkerURL)

	// The pipeline is only ready if BOTH the key and the routing URL exist
	data.OCRPipelineReady = hasGCP && cvWorkerURL != ""

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
		log.Println("No .env file found, relying on system environment variables")
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

	router := mux.NewRouter()

	// Auth routes (public)
	router.HandleFunc("/api/login", login).Methods("POST")
	router.HandleFunc("/api/force-change-password", forceChangePassword).Methods("POST")
	router.HandleFunc("/api/logout", logout).Methods("POST")
	router.HandleFunc("/api/check-auth", checkAuth).Methods("GET")
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
	router.HandleFunc("/api/accountability/storm-attendance", authMiddleware(handleStormAttendanceForDate)).Methods("GET")
	router.HandleFunc("/api/accountability/members", authMiddleware(handleAccountabilityMembers)).Methods("GET")
	router.HandleFunc("/api/accountability/members/{id:[0-9]+}", authMiddleware(handleAccountabilityMemberProfile)).Methods("GET")
	router.HandleFunc("/api/accountability/report-data", authMiddleware(handleAccountabilityReportData)).Methods("GET")
	router.HandleFunc("/api/accountability/strikes", authMiddleware(requirePermission("manage_accountability", handleStrikeCreate))).Methods("POST")
	router.HandleFunc("/api/accountability/strikes/{id:[0-9]+}", authMiddleware(requirePermission("manage_accountability", handleStrikeUpdate))).Methods("PUT")
	router.HandleFunc("/api/accountability/strikes/{id:[0-9]+}", authMiddleware(requirePermission("manage_accountability", handleStrikeDelete))).Methods("DELETE")
	router.HandleFunc("/api/accountability/storm-attendance", authMiddleware(requirePermission("manage_accountability", handleStormAttendanceUpsert))).Methods("POST")
	router.HandleFunc("/api/train-logs/{id:[0-9]+}/showed-up", authMiddleware(requirePermission("manage_accountability", handleTrainNoShow))).Methods("PUT")

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

	// Schedule API (single alliance schedule)
	router.HandleFunc("/api/schedule", authMiddleware(requirePermission("view_schedule", getSchedule))).Methods("GET")
	router.HandleFunc("/api/schedule", authMiddleware(requirePermission("manage_schedule", putSchedule))).Methods("PUT")

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
	router.HandleFunc("/api/members/import", authMiddleware(requirePermission("manage_members", importCSV))).Methods("POST")
	router.HandleFunc("/api/members/import/confirm", authMiddleware(requirePermission("manage_members", confirmMemberUpdates))).Methods("POST")

	// Prospects API
	router.HandleFunc("/api/prospects", authMiddleware(requirePermission("view_recruiting", getProspects))).Methods("GET")
	router.HandleFunc("/api/prospects", authMiddleware(requirePermission("manage_recruiting", createProspect))).Methods("POST")
	router.HandleFunc("/api/prospects/{id:[0-9]+}", authMiddleware(requirePermission("manage_recruiting", updateProspect))).Methods("PUT")
	router.HandleFunc("/api/prospects/{id:[0-9]+}", authMiddleware(requirePermission("manage_recruiting", deleteProspect))).Methods("DELETE")

	// Allies API
	router.HandleFunc("/api/ally-agreement-types", authMiddleware(getAllyAgreementTypes)).Methods("GET")
	router.HandleFunc("/api/ally-agreement-types", authMiddleware(requirePermission("manage_allies", createAllyAgreementType))).Methods("POST")
	router.HandleFunc("/api/ally-agreement-types/{id:[0-9]+}", authMiddleware(requirePermission("manage_allies", updateAllyAgreementType))).Methods("PUT")
	router.HandleFunc("/api/ally-agreement-types/{id:[0-9]+}", authMiddleware(requirePermission("manage_allies", deleteAllyAgreementType))).Methods("DELETE")
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
	router.HandleFunc("/api/vs-points", authMiddleware(getVSPoints)).Methods("GET")
	router.HandleFunc("/api/vs-points", authMiddleware(requirePermission("manage_vs_points", saveVSPoints))).Methods("POST")
	router.HandleFunc("/api/vs-points/{week}", authMiddleware(requirePermission("manage_vs_points", deleteWeekVSPoints))).Methods("DELETE")
	router.HandleFunc("/api/vs-points/process-screenshot", authMiddleware(requirePermission("manage_vs_points", processVSPointsScreenshot))).Methods("POST")

	router.HandleFunc("/api/vs-points/import/preview", authMiddleware(requirePermission("manage_members", previewCSVImport))).Methods("POST")
	router.HandleFunc("/api/vs-points/import/commit", authMiddleware(requirePermission("manage_members", commitCSVImport))).Methods("POST")

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
	router.HandleFunc("/api/power-history/process-screenshot", authMiddleware(requirePermission("manage_members", processPowerScreenshot))).Methods("POST")

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
		if auth, ok := session.Values["authenticated"].(bool); ok && auth {
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
		"/members": "members",
		"/dyno":    "dyno",
		"/rankings": "rankings",
		"/storm":    "storm",
		"/vs":       "vs",
		"/upload":   "upload",
		"/settings": "settings",
		"/admin":    "admin",
		"/profile":  "profile",
		"/files":       "files",
		"/schedule":    "schedule",
		"/alias-audit":     "alias-audit",
		"/officer-command": "officer-command",
		"/train":           "train",
		"/recruiting":      "recruiting",
		"/allies":          "allies",
		"/activity":        "activity",
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
				"dyno":     data.Permissions.ViewDyno,
				"rankings": data.Permissions.ViewRankings,
				"storm":    data.Permissions.ViewStorm,
				"vs":       data.Permissions.ViewVSPoints,
				"upload":   data.Permissions.ViewUpload,
				"settings": data.Permissions.ManageSettings,
				"admin":       data.IsAdmin,
				"schedule":    data.Permissions.ViewSchedule,
				"alias-audit":     data.Permissions.ManageMembers,
				"officer-command": data.Permissions.ViewOfficerCommand,
				"train":           data.Permissions.ViewTrain,
				"recruiting":      data.Permissions.ViewRecruiting,
				"allies":          data.Permissions.ViewAllies,
				"activity":        data.Permissions.ViewActivity || data.IsAdmin,
			"accountability":  data.Permissions.ViewAccountability,
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

		// The file exists (like style.css or app.js), serve it normally
		http.FileServer(http.Dir("static")).ServeHTTP(w, r)
	})

	// 4. Initialize CSRF Protection
	var csrfKey []byte
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
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
	slog.Info("Server listening", "port", port)
	log.Fatal(http.ListenAndServe(":"+port, appHandler)) // Use the new conditional appHandler
}
