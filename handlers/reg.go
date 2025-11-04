package handlers

import (
	"encoding/json"
	"exunreg25/db"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Participant struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Class int    `json:"class"`
	Phone string `json:"phone"`
}

type RegistrationRequest struct {
	EventID string        `json:"id"`
	Data    []Participant `json:"data"`
}

type RegistrationResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

func SubmitRegistrations(w http.ResponseWriter, r *http.Request) {
	// Block registrations while a sheets sync is in progress to avoid conflicts.
	sheetsOpMu.Lock()
	defer sheetsOpMu.Unlock()
	if r.Method != http.MethodPost {
		response := Response{
			Status: "error",
			Error:  "Method not allowed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(response)
		return
	}

	if !globalAuthHandler.isAuthenticated(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	email := globalAuthHandler.getAuthenticatedUser(r)
	userData, err := globalDB.Get("users", email)
	if err != nil || userData == nil {
		http.Redirect(w, r, "/complete", http.StatusSeeOther)
		return
	}

	user := userData.(*db.User)

	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(false)
		return
	}

	idVal, ok := raw["id"]
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(false)
		return
	}
	reqEventID := fmt.Sprintf("%v", idVal)

	var actionStr string
	if av, ok := raw["action"]; ok {
		actionStr = fmt.Sprintf("%v", av)
	}

	dataVal, ok := raw["data"]
	if !ok {
		dataVal = nil
	}
	dataArr, ok := dataVal.([]interface{})
	if !ok && dataVal != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(false)
		return
	}

	eventData, err := globalDB.Get("events", reqEventID)
	var event *db.Event
	if err != nil {
		evt, jerr := loadEventFromJSON(reqEventID)
		if jerr != nil || evt == nil {
			response := Response{
				Status: "error",
				Error:  "Event not found",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(response)
			return
		}
		event = evt
	}

	if eventData != nil {
		if ev, ok := eventData.(*db.Event); ok {
			event = ev
		}
	}
	if !event.IndependentRegistration && user.Individual {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(false)
		return
	}

	if actionStr == "delete" {
		if user.Registrations != nil {
			delete(user.Registrations, reqEventID)
		}
		user.UpdatedAt = time.Now()
		if err := globalDB.Update("users", email, user); err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(false)
			return
		}
		if regs, err := globalDB.GetAll("registrations"); err == nil {
			for _, rr := range regs {
				if r, ok := rr.(*db.Registration); ok {
					if r.EventID == reqEventID && r.UserID == user.ID {
						_ = globalDB.Delete("registrations", strconv.Itoa(r.ID))
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(true)
		return
	}

	if len(dataArr) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(false)
		return
	}
	if len(dataArr) > event.Participants {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(false)
		return
	}

	localParts := make([]Participant, 0, len(dataArr))
	for _, item := range dataArr {
		m, ok := item.(map[string]interface{})
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(Response{Status: "error", Error: "invalid participant data"})
			return
		}
		name := fmt.Sprintf("%v", m["name"])
		emailVal := fmt.Sprintf("%v", m["email"])

		var classInt int
		switch cv := m["class"].(type) {
		case float64:
			classInt = int(cv)
		case string:
			ci, err := strconv.Atoi(strings.TrimSpace(cv))
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(Response{Status: "error", Error: "invalid class value"})
				return
			}
			classInt = ci
		default:
			ci, err := strconv.Atoi(fmt.Sprintf("%v", cv))
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(Response{Status: "error", Error: "invalid class value"})
				return
			}
			classInt = ci
		}

		phoneVal := fmt.Sprintf("%v", m["phone"])

		localParts = append(localParts, Participant{
			Name:  name,
			Email: emailVal,
			Class: classInt,
			Phone: phoneVal,
		})
	}

	if err := validateParticipants(localParts, event); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Response{Status: "error", Error: err.Error()})
		return
	}

	participants := make([]db.Participant, 0, len(localParts))
	for _, p := range localParts {
		participants = append(participants, db.Participant{
			Name:  strings.ToUpper(strings.TrimSpace(p.Name)),
			Email: strings.TrimSpace(p.Email),
			Class: p.Class,
			Phone: strings.TrimSpace(p.Phone),
		})
	}

	if user.Registrations == nil {
		user.Registrations = make(map[string][]db.Participant)
	}

	user.Registrations[reqEventID] = participants
	user.UpdatedAt = time.Now()

	if err := globalDB.Update("users", email, user); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(false)
		return
	}

	registration := &db.Registration{
		EventID:   reqEventID,
		UserID:    user.ID,
		TeamName:  "",
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := globalDB.Create("registrations", registration); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(false)
		return
	}

	// Immediately mirror this user's updated state to the Google Sheet (DB is source of truth)
	if err := UpdateUsersSheetRowForEmail(globalDB, email); err != nil {
		// Non-fatal for the API; log and continue
		fmt.Printf("sheet push failed for %s: %v\n", email, err)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(true)
}

func validateParticipants(participants []Participant, event *db.Event) error {
	if len(participants) > event.Participants {
		return fmt.Errorf("maximum %d participants allowed", event.Participants)
	}
	if len(participants) == 0 {
		return fmt.Errorf("at least one participant required")
	}
	for i, participant := range participants {
		if err := validateParticipant(participant, event); err != nil {
			return fmt.Errorf("participant %d: %v", i+1, err)
		}
	}
	return nil
}

func validateParticipant(participant Participant, event *db.Event) error {
	participant.Name = strings.TrimSpace(strings.ToUpper(participant.Name))
	if participant.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !validateEmailFormat(participant.Email) {
		return fmt.Errorf("invalid email format: %s", participant.Email)
	}
	if participant.Class < 1 || participant.Class > 12 {
		return fmt.Errorf("class must be between 1 and 12")
	}
	var minClass, maxClass int
	if event.OpenToAll {
		minClass = 1
		maxClass = 12
	} else {
		var eligibility []int
		if err := json.Unmarshal([]byte(event.Eligibility), &eligibility); err == nil && len(eligibility) == 2 {
			minClass = eligibility[0]
			maxClass = eligibility[1]
		} else {
			re := regexp.MustCompile(`(\d{1,2}).*?(\d{1,2})`)
			m := re.FindStringSubmatch(event.Eligibility)
			if len(m) >= 3 {
				minClass, _ = strconv.Atoi(m[1])
				maxClass, _ = strconv.Atoi(m[2])
			} else {
				return fmt.Errorf("invalid event eligibility format")
			}
		}
	}

	if participant.Class < minClass || participant.Class > maxClass {
		return fmt.Errorf("class %d is not eligible for this event (eligible: %d-%d)", participant.Class, minClass, maxClass)
	}
	if participant.Class < minClass || participant.Class > maxClass {
		return fmt.Errorf("class %d is not eligible for this event (eligible: %d-%d)", participant.Class, minClass, maxClass)
	}
	if len(participant.Phone) != 10 {
		return fmt.Errorf("phone number must be 10 digits")
	}
	if _, err := strconv.Atoi(participant.Phone); err != nil {
		return fmt.Errorf("phone number must contain only digits")
	}

	return nil
}

func validateEmailFormat(email string) bool {
	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	matched, _ := regexp.MatchString(pattern, email)
	return matched
}

func loadEventFromJSON(eventID string) (*db.Event, error) {
	// Always load from JSON-backed list
	events, err := GetAllEventsData()
	if err != nil {
		return nil, fmt.Errorf("failed to load events json: %v", err)
	}
	for _, ev := range events {
		if ev.ID == eventID || slugify(ev.Name) == eventID {
			// return a copy
			e := ev
			return &e, nil
		}
	}
	return nil, fmt.Errorf("event not found")
}
