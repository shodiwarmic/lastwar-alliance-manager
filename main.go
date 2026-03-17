package main

import (
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
			data.Permissions = RankPermissions{ViewTrain: true, ManageTrain: true, ViewAwards: true, ManageAwards: true, ViewRecs: true, ManageRecs: true, ViewDyno: true, ManageDyno: true, ViewRankings: true, ViewStorm: true, ManageStorm: true, ViewVSPoints: true, ManageVSPoints: true, ViewUpload: true, ManageMembers: true, ManageSettings: true, ViewFiles: true, ManageFiles: true, UploadFiles: true}
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
	router.HandleFunc("/api/members/{id}/create-user", authMiddleware(requirePermission("manage_settings", createUserForMember))).Methods("POST")

	// Admin routes (admin only)
	router.HandleFunc("/api/admin/users", authMiddleware(adminMiddleware(getAdminUsers))).Methods("GET")
	router.HandleFunc("/api/admin/users", authMiddleware(adminMiddleware(createAdminUser))).Methods("POST")
	router.HandleFunc("/api/admin/users/{id}", authMiddleware(adminMiddleware(updateAdminUser))).Methods("PUT")
	router.HandleFunc("/api/admin/users/{id}", authMiddleware(adminMiddleware(deleteAdminUser))).Methods("DELETE")
	router.HandleFunc("/api/admin/users/{id}/reset-password", authMiddleware(adminMiddleware(resetUserPassword))).Methods("POST")
	router.HandleFunc("/api/admin/login-history", authMiddleware(adminMiddleware(getLoginHistory))).Methods("GET")
	router.HandleFunc("/api/admin/users/{id}/file-count", authMiddleware(adminMiddleware(getUserFileCount))).Methods("GET")
	router.HandleFunc("/api/admin/users/{id}/transfer-files", authMiddleware(adminMiddleware(transferUserFiles))).Methods("POST")

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

	// Permission Matrix & Settings routes
	router.HandleFunc("/api/permissions", authMiddleware(requirePermission("manage_settings", getPermissionsMatrix))).Methods("GET")
	router.HandleFunc("/api/permissions", authMiddleware(requirePermission("manage_settings", updatePermissionsMatrix))).Methods("PUT")
	router.HandleFunc("/api/settings", authMiddleware(getSettings)).Methods("GET")
	router.HandleFunc("/api/settings", authMiddleware(requirePermission("manage_settings", updateSettings))).Methods("PUT")

	// Members API
	router.HandleFunc("/api/members", authMiddleware(getMembers)).Methods("GET")
	router.HandleFunc("/api/members/stats", authMiddleware(getMemberStats)).Methods("GET")
	router.HandleFunc("/api/members", authMiddleware(requirePermission("manage_members", createMember))).Methods("POST")
	router.HandleFunc("/api/members/{id}", authMiddleware(requirePermission("manage_members", updateMember))).Methods("PUT")
	router.HandleFunc("/api/members/{id}", authMiddleware(requirePermission("manage_members", deleteMember))).Methods("DELETE")
	router.HandleFunc("/api/members/import", authMiddleware(requirePermission("manage_members", importCSV))).Methods("POST")
	router.HandleFunc("/api/members/import/confirm", authMiddleware(requirePermission("manage_members", confirmMemberUpdates))).Methods("POST")
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
	router.HandleFunc("/api/power-history", authMiddleware(getPowerHistory)).Methods("GET")
	router.HandleFunc("/api/power-history", authMiddleware(requirePermission("manage_members", addPowerRecord))).Methods("POST")
	router.HandleFunc("/api/power-history/process-screenshot", authMiddleware(requirePermission("manage_members", processPowerScreenshot))).Methods("POST")

	// --- UI Routes ---

	// 1. Custom 404 Handler
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := getPageData(r, "404 Not Found - Alliance Manager", "404")
		w.WriteHeader(http.StatusNotFound)
		renderTemplate(w, r, "404.html", data)
	})

	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := getPageData(r, "Members - Alliance Manager", "members")
		if !data.IsAuthenticated {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}
		renderTemplate(w, r, "members.html", data)
	}).Methods("GET")

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
		"/dyno":     "dyno",
		"/rankings": "rankings",
		"/storm":    "storm",
		"/vs":       "vs",
		"/upload":   "upload",
		"/settings": "settings",
		"/admin":    "admin",
		"/profile":  "profile",
		"/files":    "files",
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
				"admin":    data.IsAdmin,
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
	sessionKey := os.Getenv("SESSION_KEY")
	if len(sessionKey) < 32 {
		slog.Warn("SESSION_KEY is less than 32 bytes. Padding for CSRF.")
		sessionKey = sessionKey + strings.Repeat("x", 32-len(sessionKey))
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

	csrfMiddleware := csrf.Protect([]byte(sessionKey[:32]), csrfOpts...)

	// Create the protected router handler
	protectedHandler := csrfMiddleware(router)

	// Create a conditional handler to bypass CSRF for WOPI webhooks
	appHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wopi/") {
			// Send WOPI traffic directly to the router (bypassing CSRF)
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
