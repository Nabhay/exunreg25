package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"exunreg25/db"
)

func slugify(s string) string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

type Response struct {
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := Response{
		Status:  "ok",
		Message: "Server is running",
		Data: map[string]interface{}{
			"timestamp": time.Now().UTC(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func RegisterUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var userData struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}

	if err := json.NewDecoder(r.Body).Decode(&userData); err != nil {
		response := Response{
			Status: "error",
			Error:  "Invalid request body",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	user := &db.User{
		Username:     userData.Username,
		Email:        userData.Email,
		PasswordHash: "",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := globalDB.Create("users", user); err != nil {
		response := Response{
			Status: "error",
			Error:  "Failed to create user",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := Response{
		Status:  "success",
		Message: "User registered successfully",
		Data:    user,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func LoginUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response := Response{Status: "error", Error: "Invalid request body"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.Email == "" || req.Password == "" {
		response := Response{Status: "error", Error: "Email and password required"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	userIface, err := globalDB.Get("users", req.Email)
	if err != nil || userIface == nil {
		response := Response{Status: "error", Error: "Invalid email or password"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}

	user := userIface.(*db.User)
	if user.PasswordHash == "" {
		response := Response{Status: "error", Error: "Password login not configured for this account"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}

	salt := ""
	if s := os.Getenv("AUTH_SALT"); s != "" {
		salt = s
	}
	h := sha256.Sum256([]byte(salt + req.Password))
	hashed := hex.EncodeToString(h[:])

	if hashed != user.PasswordHash {
		response := Response{Status: "error", Error: "Invalid email or password"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}

	tokenData := req.Email + salt
	tokenHash := sha256.Sum256([]byte(tokenData))
	authToken := hex.EncodeToString(tokenHash[:])

	cookieSecure := false
	if os.Getenv("COOKIE_SECURE") == "true" {
		cookieSecure = true
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "email",
		Value:    req.Email,
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    authToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	response := Response{Status: "success", Message: "Logged in", Data: map[string]interface{}{"email": req.Email, "token": authToken}}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func GetUserProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	email := r.URL.Query().Get("email")
	if email == "" {
		response := Response{
			Status: "error",
			Error:  "Email parameter required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	user, err := globalDB.Get("users", email)
	if err != nil {
		response := Response{
			Status: "error",
			Error:  "User not found",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := Response{
		Status:  "success",
		Message: "User profile retrieved successfully",
		Data:    user,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func GetAllEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	email := ""
	if c, err := r.Cookie("email"); err == nil {
		email = c.Value
	}

	var eventsList []db.Event
	var err error
	if IsAdminEmail(email) {
		eventsList, err = GetAllEventsData()
	} else {
		eventsList, err = GetAllEventsForUser(email)
	}
	if err != nil {
		response := Response{Status: "error", Error: "Failed to retrieve events from DB"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	regCounts := map[string]int{}
	if regs, err := globalDB.GetAll("registrations"); err == nil {
		for _, rr := range regs {
			if r, ok := rr.(*db.Registration); ok {
				regCounts[r.EventID]++
			}
		}
	}

	events := []map[string]interface{}{}
	for _, ev := range eventsList {
		event := map[string]interface{}{
			"id":                   ev.ID,
			"name":                 ev.Name,
			"image":                ev.Image,
			"slug":                 ev.ID,
			"description_short":    ev.DescriptionShort,
			"description_long":     ev.DescriptionLong,
			"participants":         ev.Participants,
			"display_participants": ev.DisplayParticipants,
			"mode":                 ev.Mode,
			"points":               ev.Points,
			"individual":           ev.IndependentRegistration,
			"eligibility":          ev.Eligibility,
			"open_to_all":          ev.OpenToAll,
			"dates":                ev.Dates,
			"registrations":        regCounts[ev.ID],
		}
		events = append(events, event)
	}

	response := Response{Status: "success", Message: "Events retrieved successfully", Data: events}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func GetEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	eventID := r.URL.Query().Get("id")
	if eventID == "" {
		response := Response{
			Status: "error",
			Error:  "Event ID parameter required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Always read from JSON-backed events
	events, err := GetAllEventsData()
	if err == nil {
		for _, dbEv := range events {
			if dbEv.ID == eventID || slugify(dbEv.Name) == eventID {
				foundEvent := map[string]interface{}{
					"id":                   dbEv.ID,
					"name":                 dbEv.Name,
					"image":                dbEv.Image,
					"slug":                 dbEv.ID,
					"description_short":    dbEv.DescriptionShort,
					"description_long":     dbEv.DescriptionLong,
					"participants":         dbEv.Participants,
					"display_participants": dbEv.DisplayParticipants,
					"mode":                 dbEv.Mode,
					"points":               dbEv.Points,
					"individual":           dbEv.IndependentRegistration,
					"eligibility":          dbEv.Eligibility,
					"open_to_all":          dbEv.OpenToAll,
					"dates":                dbEv.Dates,
				}
				response := Response{Status: "success", Message: "Event retrieved successfully", Data: foundEvent}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(response)
				return
			}
		}
	}

	response := Response{Status: "error", Error: "Event not found"}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(response)
}

var globalDB *db.Database

func SetGlobalDB(database *db.Database) {
	globalDB = database
	go startSheetsSync(database)
}

func GetAllEventsData() ([]db.Event, error) {
	// Always load events from JSON; do not rely on DB events table.
	b, err := os.ReadFile("frontend/data/events.json")
	if err != nil {
		if b2, err2 := os.ReadFile("data/events.json"); err2 == nil {
			b = b2
		} else {
			return nil, err
		}
	}
	// Parse a subset of fields to db.Event list
	var raw struct {
		Events  map[string]string `json:"events"`
		Default struct {
			OpenToAll    bool   `json:"open_to_all"`
			Eligibility  []int  `json:"eligibility"`
			Participants int    `json:"participants"`
			Mode         string `json:"mode"`
			Descriptions struct {
				Long  string `json:"long"`
				Short string `json:"short"`
			} `json:"descriptions"`
			IndependentRegistrations bool   `json:"independent_registrations"`
			Points                   int    `json:"points"`
			Dates                    string `json:"dates"`
		} `json:"default"`
		Descriptions map[string]struct {
			Long  string `json:"long"`
			Short string `json:"short"`
		} `json:"descriptions"`
		Participants map[string]int    `json:"participants"`
		Mode         map[string]string `json:"mode"`
		Points       map[string]int    `json:"points"`
		Individual   map[string]bool   `json:"individual"`
		Display      map[string]string `json:"display_participants"`
		Eligibility  map[string][]int  `json:"eligibility"`
		OpenToAll    map[string]bool   `json:"open_to_all"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	events := make([]db.Event, 0, len(raw.Events))
	// Preserve order: iterate the JSON again to keep declared order
	dec := json.NewDecoder(strings.NewReader(string(b)))
	var ordered []string
	if tok, err := dec.Token(); err == nil {
		for dec.More() {
			k, _ := dec.Token()
			key, _ := k.(string)
			if key == "events" {
				// open object
				_, _ = dec.Token()
				for dec.More() {
					kn, _ := dec.Token()
					name, _ := kn.(string)
					var skip string
					_ = dec.Decode(&skip)
					ordered = append(ordered, name)
				}
				_, _ = dec.Token()
				break
			} else {
				var skip interface{}
				_ = dec.Decode(&skip)
			}
		}
		_ = tok
	}
	if len(ordered) == 0 {
		for name := range raw.Events {
			ordered = append(ordered, name)
		}
	}
	now := time.Now()
	for _, name := range ordered {
		slug := slugify(name)
		participants := raw.Default.Participants
		if v, ok := raw.Participants[name]; ok {
			participants = v
		}
		mode := raw.Default.Mode
		if v, ok := raw.Mode[name]; ok {
			mode = v
		}
		points := raw.Default.Points
		if v, ok := raw.Points[name]; ok {
			points = v
		}
		individual := raw.Default.IndependentRegistrations
		if v, ok := raw.Individual[name]; ok {
			individual = v
		}
		descShort := raw.Default.Descriptions.Short
		descLong := raw.Default.Descriptions.Long
		if v, ok := raw.Descriptions[name]; ok {
			if v.Short != "" {
				descShort = v.Short
			}
			if v.Long != "" {
				descLong = v.Long
			}
		}
		openAll := raw.Default.OpenToAll
		if v, ok := raw.OpenToAll[name]; ok {
			openAll = v
		}
		eligibility := ""
		if openAll {
			eligibility = "Open to all"
		} else if vals, ok := raw.Eligibility[name]; ok && len(vals) >= 2 {
			eligibility = fmt.Sprintf("Grades %d–%d", vals[0], vals[1])
		} else if len(raw.Default.Eligibility) >= 2 {
			eligibility = fmt.Sprintf("Grades %d–%d", raw.Default.Eligibility[0], raw.Default.Eligibility[1])
		}
		// lookup custom display participants text if present
		disp := ""
		if v, ok := raw.Display[name]; ok {
			disp = v
		}

		events = append(events, db.Event{
			ID:                      slug,
			Name:                    name,
			Image:                   raw.Events[name],
			OpenToAll:               openAll,
			Eligibility:             eligibility,
			Participants:            participants,
			DisplayParticipants:     disp,
			Mode:                    mode,
			IndependentRegistration: individual,
			Points:                  points,
			Dates:                   raw.Default.Dates,
			DescriptionLong:         descLong,
			DescriptionShort:        descShort,
			CreatedAt:               now,
			UpdatedAt:               now,
		})
	}
	return events, nil
}

func GetAllEventsForUser(email string) ([]db.Event, error) {
	events, err := GetAllEventsData()
	if err != nil {
		return nil, err
	}
	if email == "" {
		return events, nil
	}
	uIface, err := globalDB.Get("users", email)
	if err != nil || uIface == nil {
		return events, nil
	}
	user := uIface.(*db.User)
	if !user.Individual {
		return events, nil
	}
	filtered := make([]db.Event, 0, len(events))
	for _, ev := range events {
		if ev.IndependentRegistration {
			filtered = append(filtered, ev)
		}
	}
	return filtered, nil
}
