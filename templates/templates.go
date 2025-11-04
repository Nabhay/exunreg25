package templates

import (
	"html/template"
	"net/http"
	"path/filepath"
	"strings"

	"exunreg25/handlers"
)

var templates *template.Template

type TemplateData struct {
	IsAuthenticated  bool
	IsAdmin          bool
	IsHome           bool
	IsEvents         bool
	IsBrochure       bool
	IsQuery          bool
	BrochureMarkdown string
	BrochureNavHTML  template.HTML
	BrochureTOC      string
	BrochureScrollTo string
	User             *User
	Event            *Event
	Events           []Event
	Categories       []Category
	Stats            *AdminStats
	Summary          *Summary
	PageTitle        string
	CurrentPath      string
	Sponsors         []Sponsor
}

type Category struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type User struct {
	Email           string `json:"email"`
	Fullname        string `json:"fullname"`
	PhoneNumber     string `json:"phone_number"`
	PrincipalsEmail string `json:"principals_email"`
	InstitutionName string `json:"institution_name"`
	Address         string `json:"address"`
	PrincipalsName  string `json:"principals_name"`
	Individual      bool   `json:"individual"`
	Registrations   map[string][]Participant
}

type Participant struct {
	Name  string `json:"name"`
	Class int    `json:"class"`
}

type Summary struct {
	TotalRegistrations     int `json:"total_registrations"`
	ConfirmedRegistrations int `json:"confirmed_registrations"`
	PendingRegistrations   int `json:"pending_registrations"`
}

type Event struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	DescriptionShort string `json:"description_short"`
	DescriptionLong  string `json:"description_long"`
	EligibilityText  string `json:"eligibility_text"`
	Mode             string `json:"mode"`
	Image            string `json:"image"`
	Participants     int    `json:"participants"`
	MaxParticipants  int    `json:"max_participants"`
	IsRegistered     bool   `json:"is_registered"`
	Points           int    `json:"points"`
	Individual       bool   `json:"individual"`
	Dates            string `json:"dates"`
}

type AdminStats struct {
	TotalUsers         int                   `json:"total_users"`
	TotalEvents        int                   `json:"total_events"`
	TotalRegistrations int                   `json:"total_registrations"`
	EventStats         map[string]EventStats `json:"event_stats"`
}

type EventStats struct {
	EventName         string `json:"event_name"`
	TotalParticipants int    `json:"total_participants"`
	TotalTeams        int    `json:"total_teams"`
	Mode              string `json:"mode"`
	Eligibility       string `json:"eligibility"`
}

type Sponsor struct {
	ImageURL string `json:"imageUrl"`
	Alt      string `json:"alt"`
	Link     string `json:"link"`
	Height   string `json:"height"`
}

func InitTemplates() error {
	var err error

	templates, err = template.ParseGlob("frontend/*.html")
	if err != nil {
		return err
	}

	componentTemplates, err := template.ParseGlob("frontend/components/*.html")
	if err != nil {
		return err
	}

	for _, tmpl := range componentTemplates.Templates() {
		templates.AddParseTree(tmpl.Name(), tmpl.Tree)
	}

	if _, err := templates.New("chat").ParseFiles("frontend/components/chat.html"); err != nil {
		return err
	}

	return nil
}

func RenderTemplate(w http.ResponseWriter, templateName string, data TemplateData) error {
	if templates == nil {
		if err := InitTemplates(); err != nil {
			return err
		}
	}

	baseName := filepath.Base(templateName)
	if filepath.Ext(baseName) == "" {
		baseName = baseName + ".html"
	}

	return templates.ExecuteTemplate(w, baseName, data)
}

func LoadEventsFromJSON() ([]Event, error) {
	dbEvents, err := handlers.GetAllEventsData()
	if err != nil {
		return nil, err
	}

	var events []Event
	for _, ev := range dbEvents {
		img := ev.Image
		if !strings.HasPrefix(img, "/") {
			img = "/illustrations/" + img
		}
		events = append(events, Event{
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

	return events, nil
}

func FindEventBySlug(slug string) (*Event, error) {
	events, err := LoadEventsFromJSON()
	if err != nil {
		return nil, err
	}

	for _, event := range events {
		if event.Slug == slug {
			return &event, nil
		}
	}

	return nil, nil
}
