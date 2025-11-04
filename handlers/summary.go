package handlers

import (
	"encoding/json"
	"exunreg25/db"
	"net/http"
)

type SummaryResponse struct {
	Status  string                 `json:"status"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

type EventSummary struct {
	EventID      string           `json:"event_id"`
	EventName    string           `json:"event_name"`
	Participants []db.Participant `json:"participants"`
	TotalCount   int              `json:"total_count"`
	Status       string           `json:"status"`
	Capacity     int              `json:"capacity"`
}

func GetUserSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response := SummaryResponse{
			Status:  "error",
			Message: "Method not allowed",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(response)
		return
	}

	if !globalAuthHandler.isAuthenticated(r) {
		response := SummaryResponse{
			Status:  "error",
			Message: "Authentication required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(response)
		return
	}

	email := globalAuthHandler.getAuthenticatedUser(r)
	userData, err := globalDB.Get("users", email)
	if err != nil {
		response := SummaryResponse{
			Status:  "error",
			Message: "User not found",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	user := userData.(*db.User)
	if user.Username == "" {
		response := SummaryResponse{
			Status:  "error",
			Message: "Complete signup required",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(response)
		return
	}

	if user.Registrations == nil {
		user.Registrations = make(map[string][]db.Participant)
	}

	eventSummaries := []EventSummary{}
	totalParticipants := 0
	pendingCount := 0
	totalRegistrations := 0

	if evs, err := GetAllEventsData(); err == nil {
		for _, ev := range evs {
			// For individual users, show only independent-registration events
			if user.Individual && !ev.IndependentRegistration {
				continue
			}
			eventID := ev.ID
			parts := []db.Participant{}
			if user.Registrations != nil {
				if p, ok := user.Registrations[eventID]; ok {
					parts = p
				}
			}
			status := "confirmed"
			if len(parts) == 0 {
				status = "pending"
				pendingCount++
			} else {
				totalParticipants += len(parts)
				totalRegistrations++
			}

			eventSummaries = append(eventSummaries, EventSummary{
				EventID:      eventID,
				EventName:    ev.Name,
				Participants: parts,
				TotalCount:   len(parts),
				Status:       status,
				Capacity:     ev.Participants,
			})
		}
	}

	summaryData := map[string]interface{}{
		"total_events_registered": totalRegistrations,
		"total_participants":      totalParticipants,
		"pending_registrations":   pendingCount,
		"events":                  eventSummaries,
		"user_info": map[string]interface{}{
			"fullname": user.Fullname,
			"email":    user.Email,
			"individual_info": map[string]interface{}{
				"phone_number": user.PhoneNumber,
				"address":      user.Address,
			},
			"individual": user.Individual,
		},
	}

	if totalRegistrations == 0 {
		summaryData["message"] = "No registrations found"
	} else {
		summaryData["message"] = "Registration summary retrieved successfully"
	}

	response := SummaryResponse{
		Status:  "success",
		Message: "Registration summary",
		Data:    summaryData,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
