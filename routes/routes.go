package routes

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"exunreg25/handlers"
	"exunreg25/middleware"
	"exunreg25/templates"
	"mime"
	"net/http"
)

func slugify(s string) string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func getTemplateData(r *http.Request) templates.TemplateData {
	data := templates.TemplateData{
		IsAuthenticated: middleware.IsAuthenticated(r),
		IsAdmin:         false,
		CurrentPath:     r.URL.Path,
	}

	if data.IsAuthenticated {
		if email := middleware.GetEmailFromCookie(r); email != "" {
			data.IsAdmin = handlers.IsAdminEmail(email)
		}
	}

	data.IsHome = (r.URL.Path == "/" || r.URL.Path == "/index")
	data.IsEvents = (r.URL.Path == "/events")
	data.IsBrochure = strings.HasPrefix(r.URL.Path, "/brochure")
	data.IsQuery = (r.URL.Path == "/query")

	if b, err := os.ReadFile("frontend/data/sponsors.json"); err == nil {
		var s []templates.Sponsor
		if err2 := json.Unmarshal(b, &s); err2 == nil {
			data.Sponsors = s
		}
	} else if b2, err2 := os.ReadFile("data/sponsors.json"); err2 == nil {
		var s []templates.Sponsor
		if err3 := json.Unmarshal(b2, &s); err3 == nil {
			data.Sponsors = s
		}
	}

	return data
}

func escapeForJSLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "$", "\\$")
	return s
}

func SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("/assets/", http.StripPrefix("/assets/", customFileServer("./frontend/assets/")))
	mux.Handle("/css/", http.StripPrefix("/css/", customFileServer("./frontend/css/")))
	mux.Handle("/js/", http.StripPrefix("/js/", customFileServer("./frontend/js/")))
	mux.Handle("/illustrations/", http.StripPrefix("/illustrations/", customFileServer("./frontend/illustrations/")))
	mux.Handle("/data/", http.StripPrefix("/data/", customFileServer("./frontend/data/")))
	mux.Handle("/fonts/", http.StripPrefix("/fonts/", customFileServer("./frontend/fonts/")))
	mux.Handle("/components/", http.StripPrefix("/components/", customFileServer("./frontend/components/")))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if strings.HasPrefix(path, "/brochure/") {
			slug := strings.TrimPrefix(path, "/brochure/")
			slug = strings.Trim(slug, "/")
			data := getTemplateData(r)
			data.PageTitle = "Brochure | Exun 2025"
			if b, err := os.ReadFile("frontend/data/invite.md"); err == nil {
				data.BrochureMarkdown = string(b)
			} else if b2, err2 := os.ReadFile("data/invite.md"); err2 == nil {
				data.BrochureMarkdown = string(b2)
			}
			type tocItem struct {
				Level int
				Title string
				ID    string
			}
			var toc []tocItem
			var navBuilder strings.Builder
			lines := strings.Split(data.BrochureMarkdown, "\n")
			for _, line := range lines {
				m := regexp.MustCompile(`^(#{1,3})\s+(.*)$`).FindStringSubmatch(line)
				if len(m) == 3 {
					level := len(m[1])
					title := strings.TrimSpace(m[2])
					titleClean := strings.ReplaceAll(title, "*", "")
					titleClean = strings.ReplaceAll(titleClean, "_", "")
					id := slugify(titleClean)
					toc = append(toc, tocItem{Level: level, Title: titleClean, ID: id})
					navBuilder.WriteString(fmt.Sprintf("<li class=\"nav-level-%d\"><a href=\"/brochure/%s\">%s</a></li>", level, id, template.HTMLEscapeString(titleClean)))
				}
			}
			navJSON, _ := json.Marshal(toc)
			data.BrochureTOC = string(navJSON)
			data.BrochureNavHTML = template.HTML(navBuilder.String())
			data.BrochureScrollTo = slug
			templates.RenderTemplate(w, "brochure", data)
			return
		}
		switch path {
		case "/", "/index":
			data := getTemplateData(r)
			data.PageTitle = "Exun 2025"
			email := middleware.GetEmailFromCookie(r)
			if email != "" {
				if evs, err := handlers.GetAllEventsForUser(email); err == nil {
					tmplEvents := make([]templates.Event, 0, len(evs))
					for _, ev := range evs {
						img := ev.Image
						if !strings.HasPrefix(img, "/") {
							img = "/illustrations/" + img
						}
						tmplEvents = append(tmplEvents, templates.Event{
							ID:               ev.ID,
							Name:             ev.Name,
							Slug:             ev.ID,
							Image:            img,
							Mode:             ev.Mode,
							Participants:     ev.Participants,
							MaxParticipants:  0,
							IsRegistered:     false,
							DescriptionShort: ev.DescriptionShort,
							DescriptionLong:  ev.DescriptionLong,
							EligibilityText:  ev.Eligibility,
							Points:           ev.Points,
							Individual:       ev.IndependentRegistration,
							Dates:            ev.Dates,
						})
					}
					data.Events = tmplEvents
				}
			} else {
				if events, err := templates.LoadEventsFromJSON(); err == nil {
					data.Events = events
				}
			}
			events := data.Events
			cats := make(map[string]struct{})
			for _, e := range events {
				if e.Slug != "" {
					if strings.Contains(e.Name, "Build") {
						cats["build"] = struct{}{}
					}
					if strings.Contains(e.Name, "CubXL") {
						cats["cubing"] = struct{}{}
					}
				}
			}
			for k := range cats {
				data.Categories = append(data.Categories, templates.Category{Key: k, Name: strings.Title(k)})
			}
			templates.RenderTemplate(w, "index", data)
			return
		case "/events":
			data := getTemplateData(r)
			data.PageTitle = "Events | Exun 2025"
			email := middleware.GetEmailFromCookie(r)
			if email != "" {
				if evs, err := handlers.GetAllEventsForUser(email); err == nil {
					tmplEvents := make([]templates.Event, 0, len(evs))
					for _, ev := range evs {
						img := ev.Image
						if !strings.HasPrefix(img, "/") {
							img = "/illustrations/" + img
						}
						tmplEvents = append(tmplEvents, templates.Event{
							ID:               ev.ID,
							Name:             ev.Name,
							Slug:             ev.ID,
							Image:            img,
							Mode:             ev.Mode,
							Participants:     ev.Participants,
							MaxParticipants:  0,
							IsRegistered:     false,
							DescriptionShort: ev.DescriptionShort,
							DescriptionLong:  ev.DescriptionLong,
							EligibilityText:  ev.Eligibility,
							Points:           ev.Points,
							Individual:       ev.IndependentRegistration,
							Dates:            ev.Dates,
						})
					}
					data.Events = tmplEvents
				}
			} else {
				if events, err := templates.LoadEventsFromJSON(); err == nil {
					data.Events = events
				}
			}
			cats := make(map[string]struct{})
			for _, e := range data.Events {
				if e.Slug != "" {
					if strings.Contains(e.Name, "Build") {
						cats["build"] = struct{}{}
					}
					if strings.Contains(e.Name, "CubXL") {
						cats["cubing"] = struct{}{}
					}
				}
			}
			for k := range cats {
				data.Categories = append(data.Categories, templates.Category{Key: k, Name: strings.Title(k)})
			}
			templates.RenderTemplate(w, "events", data)
			return
		case "/brochure":
			data := getTemplateData(r)
			data.PageTitle = "Brochure | Exun 2025"
			if b, err := os.ReadFile("frontend/data/invite.md"); err == nil {
				data.BrochureMarkdown = string(b)
			} else if b2, err2 := os.ReadFile("data/invite.md"); err2 == nil {
				data.BrochureMarkdown = string(b2)
			}

			type tocItem struct {
				Level int
				Title string
				ID    string
			}
			var toc []tocItem
			var navBuilder strings.Builder
			lines := strings.Split(data.BrochureMarkdown, "\n")
			for _, line := range lines {
				m := regexp.MustCompile(`^(#{1,3})\s+(.*)$`).FindStringSubmatch(line)
				if len(m) == 3 {
					level := len(m[1])
					title := strings.TrimSpace(m[2])
					titleClean := regexp.MustCompile(`\*\*(.*?)\*\*`).ReplaceAllString(title, "$1")
					titleClean = regexp.MustCompile(`\*(.*?)\*`).ReplaceAllString(titleClean, "$1")
					id := slugify(titleClean)
					toc = append(toc, tocItem{Level: level, Title: titleClean, ID: id})
					navBuilder.WriteString(fmt.Sprintf("<li class=\"nav-level-%d\"><a href=\"/brochure/%s\">%s</a></li>", level, id, template.HTMLEscapeString(titleClean)))
				}
			}
			navJSON, _ := json.Marshal(toc)
			data.BrochureTOC = string(navJSON)
			data.BrochureNavHTML = template.HTML(navBuilder.String())

			templates.RenderTemplate(w, "brochure", data)
			return
		case "/summary":
			data := getTemplateData(r)
			if !data.IsAuthenticated {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			data.PageTitle = "Summary | Exun 2025"
			rec := httptest.NewRecorder()
			hreq := r.Clone(r.Context())
			handlers.GetUserSummary(rec, hreq)
			if rec.Code == http.StatusOK {
				var resp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err == nil {
					if dataMap, ok := resp["data"].(map[string]interface{}); ok {
						if ui, ok := dataMap["user_info"].(map[string]interface{}); ok {
							user := &templates.User{}
							if v, ok := ui["email"].(string); ok {
								user.Email = v
							}
							if v, ok := ui["fullname"].(string); ok {
								user.Fullname = v
							}
							if v, ok := ui["phone"].(string); ok {
								user.PhoneNumber = v
							}
							if v, ok := ui["institution_name"].(string); ok {
								user.InstitutionName = v
							}
							if v, ok := ui["principals_email"].(string); ok {
								user.PrincipalsEmail = v
							}
							if v, ok := ui["individual"].(bool); ok {
								user.Individual = v
							}
							data.User = user
						}

						summary := &templates.Summary{}
						if v, ok := dataMap["total_events_registered"].(float64); ok {
							summary.TotalRegistrations = int(v)
						}
						if v, ok := dataMap["total_participants"].(float64); ok {
							summary.ConfirmedRegistrations = int(v)
						}
						summary.PendingRegistrations = 0
						data.Summary = summary
					}
				}
			}
			templates.RenderTemplate(w, "summary", data)
			return
		case "/complete":
			data := getTemplateData(r)
			if !data.IsAuthenticated {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			data.PageTitle = "Complete Signup | Exun 2025"
			templates.RenderTemplate(w, "complete", data)
			return
		case "/query":
			data := getTemplateData(r)
			data.PageTitle = "Queries | Exun 2025"
			templates.RenderTemplate(w, "query", data)
			return
		case "/login":
			data := getTemplateData(r)
			data.PageTitle = "Login | Exun 2025"
			templates.RenderTemplate(w, "login", data)
			return
		}
		if strings.HasSuffix(path, ".html") {
			http.Redirect(w, r, strings.TrimSuffix(path, ".html"), http.StatusMovedPermanently)
			return
		}

		if path != "/" {
			raw := strings.TrimPrefix(path, "/")
			decoded, err := url.PathUnescape(raw)
			if err == nil && decoded != "" {
				if evs, err := handlers.GetAllEventsData(); err == nil {
					for _, ev := range evs {
						if ev.Name == decoded || slugify(ev.Name) == decoded || ev.ID == decoded {
							canonical := slugify(ev.Name)
							if decoded != canonical {
								http.Redirect(w, r, "/"+canonical, http.StatusMovedPermanently)
								return
							}
							data := getTemplateData(r)
							data.PageTitle = ev.Name + " | Exun 2025"
							if event, err := templates.FindEventBySlug(canonical); err == nil && event != nil {
								data.Event = event
							}
							templates.RenderTemplate(w, "event-detail", data)
							return
						}
					}
				}
			}
		}
		data := getTemplateData(r)
		data.PageTitle = "Page not found | Exun 2025"
		templates.RenderTemplate(w, "404", data)
	})

	mux.HandleFunc("/api/health", handlers.HealthCheck)

	mux.HandleFunc("/oauth2callback", handlers.HandleOAuth2Callback)

	mux.HandleFunc("/api/auth/send-otp", handlers.SendOTP)
	mux.HandleFunc("/api/auth/verify-otp", handlers.VerifyOTP)
	mux.HandleFunc("/api/auth/logout", handlers.Logout)
	mux.HandleFunc("/api/auth/complete", handlers.CompleteSignup)
	mux.HandleFunc("/api/auth/change-password", handlers.ChangePassword)
	mux.HandleFunc("/api/auth/reset-password", handlers.ResetPassword)

	mux.HandleFunc("/api/users/register", handlers.RegisterUser)
	mux.HandleFunc("/api/users/login", handlers.LoginUser)

	profileHandler := http.HandlerFunc(handlers.GetUserProfile)
	mux.Handle("/api/users/profile", middleware.AuthRequired(profileHandler))

	profileAuthHandler := http.HandlerFunc(handlers.GetProfile)
	mux.Handle("/api/auth/profile", middleware.AuthRequired(profileAuthHandler))

	mux.HandleFunc("/api/events", handlers.GetAllEvents)
	mux.HandleFunc("/api/events/", handlers.GetEvent)

	mux.HandleFunc("/api/query", handlers.QueryHandler)

	// Webhook endpoint for Google Sheets Apps Script row updates
	mux.HandleFunc("/api/sheets/webhook", handlers.SheetsRowUpdate)
	// Headers/Schema for apps script
	mux.HandleFunc("/api/sheets/headers", handlers.SheetsHeaders)
	// Ping for secret verification
	mux.HandleFunc("/api/sheets/ping", handlers.SheetsPing)

	submitRegHandler := http.HandlerFunc(handlers.SubmitRegistrations)
	mux.Handle("/api/submit_registrations", middleware.AuthRequired(submitRegHandler))

	completeSignupPageHandler := http.HandlerFunc(handlers.CompleteSignupPage)
	mux.Handle("/api/complete", middleware.AuthRequired(completeSignupPageHandler))

	completeSignupAPIHandler := http.HandlerFunc(handlers.CompleteSignupAPI)
	mux.Handle("/api/complete_api", middleware.AuthRequired(completeSignupAPIHandler))

	userProfileHandler := http.HandlerFunc(handlers.GetUserProfileData)
	mux.Handle("/api/user/profile", middleware.AuthRequired(userProfileHandler))

	registrationHistoryHandler := http.HandlerFunc(handlers.GetUserRegistrationHistory)
	mux.Handle("/api/user/registration_history", middleware.AuthRequired(registrationHistoryHandler))

	summaryHandler := http.HandlerFunc(handlers.GetUserSummary)
	mux.Handle("/api/summary", middleware.AuthRequired(summaryHandler))

	adminStatsHandler := http.HandlerFunc(handlers.GetAdminStats)
	mux.Handle("/api/admin/stats", middleware.AuthRequired(adminStatsHandler))

	adminConfigHandler := http.HandlerFunc(handlers.GetAdminConfig)
	mux.Handle("/api/admin/config", middleware.AuthRequired(adminConfigHandler))

	adminEventHandler := http.HandlerFunc(handlers.GetAdminEvent)
	mux.Handle("/api/admin/events/", middleware.AuthRequired(adminEventHandler))

	adminUpdateEventHandler := http.HandlerFunc(handlers.UpdateEvent)
	mux.Handle("/api/admin/events", middleware.AuthRequired(adminUpdateEventHandler))

	adminDeleteEventHandler := http.HandlerFunc(handlers.DeleteEvent)
	mux.Handle("/api/admin/events/delete/", middleware.AuthRequired(adminDeleteEventHandler))

	syncSheetsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := middleware.GetEmailFromCookie(r)
		if !handlers.IsAdminEmail(email) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := handlers.TriggerSheetsSync(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","message":"sync triggered"}`))
	})
	mux.Handle("/api/admin/sync-sheets", middleware.AuthRequired(syncSheetsHandler))

	adminUserDetailsHandler := http.HandlerFunc(handlers.GetUserDetails)
	mux.Handle("/api/admin/users", middleware.AuthRequired(adminUserDetailsHandler))

	adminEventRegistrationsHandler := http.HandlerFunc(handlers.GetEventRegistrations)
	mux.Handle("/api/admin/event-registrations", middleware.AuthRequired(adminEventRegistrationsHandler))

	adminExportHandler := http.HandlerFunc(handlers.ExportData)
	mux.Handle("/api/admin/export", middleware.AuthRequired(adminExportHandler))

	adminSendInviteHandler := http.HandlerFunc(handlers.SendInvite)
	mux.Handle("/api/admin/send-invite", middleware.AuthRequired(adminSendInviteHandler))
	adminImportEventsHandler := http.HandlerFunc(handlers.ImportEvents)
	mux.Handle("/api/admin/import_events", middleware.AuthRequired(adminImportEventsHandler))

	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		data := getTemplateData(r)
		if !data.IsAuthenticated || !data.IsAdmin {
			data.PageTitle = "Page not found | Exun 2025"
			w.WriteHeader(http.StatusNotFound)
			templates.RenderTemplate(w, "404", data)
			return
		}
		data.PageTitle = "Admin | Exun 2025"
		if stats, err := handlers.GetAdminStatsData(); err == nil {
			tmplStats := templates.AdminStats{
				TotalUsers:         stats.TotalUsers,
				TotalEvents:        stats.TotalEvents,
				TotalRegistrations: stats.TotalRegistrations,
				EventStats:         make(map[string]templates.EventStats),
			}
			for id, s := range stats.EventStats {
				tmplStats.EventStats[id] = templates.EventStats{
					EventName:         s.EventName,
					TotalParticipants: s.TotalParticipants,
					TotalTeams:        s.TotalTeams,
					Mode:              s.Mode,
					Eligibility:       s.Eligibility,
				}
			}
			data.Stats = &tmplStats
		}
		templates.RenderTemplate(w, "admin", data)
	})
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		data := getTemplateData(r)
		if !data.IsAuthenticated || !data.IsAdmin {
			data.PageTitle = "Page not found | Exun 2025"
			w.WriteHeader(http.StatusNotFound)
			templates.RenderTemplate(w, "404", data)
			return
		}
		data.PageTitle = "Admin | Exun 2025"
		templates.RenderTemplate(w, "admin", data)
	})

	return mux
}
func customFileServer(root string) http.Handler {
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ext := filepath.Ext(r.URL.Path)
		switch ext {
		case ".css":
			w.Header().Set("Content-Type", "text/css")
		case ".js":
			w.Header().Set("Content-Type", "application/javascript")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".ico":
			w.Header().Set("Content-Type", "image/x-icon")
		default:
			if mt := mime.TypeByExtension(ext); mt != "" {
				w.Header().Set("Content-Type", mt)
			}
		}
		fs.ServeHTTP(w, r)
	})
}
func SetupServer() *http.Server {
	mux := SetupRoutes()

	handler := middleware.CORS(middleware.Logger(mux))

	return &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}
}
