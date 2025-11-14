package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	*sql.DB
}

type Participant struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Class int    `json:"class"`
	Phone string `json:"phone"`
}

type User struct {
	ID              int                      `json:"id"`
	Username        string                   `json:"username"`
	Email           string                   `json:"email"`
	PasswordHash    string                   `json:"password_hash"`
	Fullname        string                   `json:"fullname"`
	PhoneNumber     string                   `json:"phone_number"`
	PrincipalsEmail string                   `json:"principals_email"`
	SchoolCode      string                   `json:"school_code"`
	Individual      bool                     `json:"individual"`
	InstitutionName string                   `json:"institution_name"`
	Address         string                   `json:"address"`
	PrincipalsName  string                   `json:"principals_name"`
	Registrations   map[string][]Participant `json:"registrations"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

func (u *User) MarshalJSON() ([]byte, error) {
	type Alias User
	return json.Marshal(&struct {
		*Alias
		Registrations string `json:"registrations"`
	}{
		Alias:         (*Alias)(u),
		Registrations: u.marshalRegistrations(),
	})
}

func (u *User) UnmarshalJSON(data []byte) error {
	type Alias User
	aux := &struct {
		*Alias
		Registrations string `json:"registrations"`
	}{
		Alias: (*Alias)(u),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	return u.unmarshalRegistrations(aux.Registrations)
}

func (u *User) marshalRegistrations() string {
	if u.Registrations == nil {
		return "{}"
	}
	data, err := json.Marshal(u.Registrations)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (u *User) unmarshalRegistrations(data string) error {
	if data == "" || data == "{}" {
		u.Registrations = make(map[string][]Participant)
		return nil
	}
	return json.Unmarshal([]byte(data), &u.Registrations)
}

type Event struct {
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	Image                   string    `json:"image"`
	OpenToAll               bool      `json:"open_to_all"`
	Eligibility             string    `json:"eligibility"`
	Participants            int       `json:"participants"`
	DisplayParticipants     string    `json:"display_participants"`
	Mode                    string    `json:"mode"`
	IndependentRegistration bool      `json:"independent_registration"`
	Points                  int       `json:"points"`
	Dates                   string    `json:"dates"`
	DescriptionLong         string    `json:"description_long"`
	DescriptionShort        string    `json:"description_short"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type Registration struct {
	ID        int       `json:"id"`
	EventID   string    `json:"event_id"`
	UserID    int       `json:"user_id"`
	TeamName  string    `json:"team_name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IndividualRegistration struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Fullname  string    `json:"fullname"`
	UserEmail string    `json:"user_email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type LogEntry struct {
	ID        int       `json:"id"`
	Reason    string    `json:"reason"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func NewConnection(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %v", err)
	}

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("error connecting to database: %v", err)
	}

	log.Println("Successfully connected to SQLite database")
	return &Database{db}, nil
}

func (db *Database) Close() error {
	return db.DB.Close()
}

func (db *Database) InitTables() error {
	createUsersTable := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		school_code TEXT,
		fullname TEXT,
		phone_number TEXT,
		principals_email TEXT,
		individual BOOLEAN DEFAULT FALSE,
		institution_name TEXT,
		address TEXT,
		principals_name TEXT,
		registrations TEXT DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createEventsTable := `
	CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		image TEXT,
		open_to_all BOOLEAN DEFAULT FALSE,
		eligibility TEXT,
		participants INTEGER DEFAULT 1,
		mode TEXT DEFAULT 'online',
		independent_registration BOOLEAN DEFAULT TRUE,
		points INTEGER DEFAULT 0,
		dates TEXT,
		description_long TEXT,
		description_short TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	createRegistrationsTable := `
	CREATE TABLE IF NOT EXISTS registrations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id TEXT NOT NULL,
		user_id INTEGER NOT NULL,
		team_name TEXT,
		status TEXT DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (event_id) REFERENCES events (id),
		FOREIGN KEY (user_id) REFERENCES users (id)
	);`

	createIndexes := `
	CREATE INDEX IF NOT EXISTS idx_events_name ON events(name);
	CREATE INDEX IF NOT EXISTS idx_registrations_event_user ON registrations(event_id, user_id);
	CREATE INDEX IF NOT EXISTS idx_registrations_status ON registrations(status);
	`

	if _, err := db.Exec(createUsersTable); err != nil {
		return fmt.Errorf("error creating users table: %v", err)
	}

	if _, err := db.Exec(createEventsTable); err != nil {
		return fmt.Errorf("error creating events table: %v", err)
	}

	if _, err := db.Exec(createRegistrationsTable); err != nil {
		return fmt.Errorf("error creating registrations table: %v", err)
	}

	createIndividualRegistrationsTable := `
	CREATE TABLE IF NOT EXISTS individual_registrations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		fullname TEXT,
		user_email TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users (id)
	);`

	if _, err := db.Exec(createIndividualRegistrationsTable); err != nil {
		return fmt.Errorf("error creating individual_registrations table: %v", err)
	}

	createLogsTable := `
	CREATE TABLE IF NOT EXISTS logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		reason TEXT NOT NULL,
		content TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err := db.Exec(createLogsTable); err != nil {
		return fmt.Errorf("error creating logs table: %v", err)
	}

	createUsrRegsTable := `
	CREATE TABLE IF NOT EXISTS usr_regs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT,
		institution TEXT,
		event_id TEXT,
		p1_name TEXT, p1_email TEXT, p1_class TEXT, p1_phone TEXT,
		p2_name TEXT, p2_email TEXT, p2_class TEXT, p2_phone TEXT,
		p3_name TEXT, p3_email TEXT, p3_class TEXT, p3_phone TEXT,
		p4_name TEXT, p4_email TEXT, p4_class TEXT, p4_phone TEXT,
		p5_name TEXT, p5_email TEXT, p5_class TEXT, p5_phone TEXT,
		p6_name TEXT, p6_email TEXT, p6_class TEXT, p6_phone TEXT,
		p7_name TEXT, p7_email TEXT, p7_class TEXT, p7_phone TEXT,
		p8_name TEXT, p8_email TEXT, p8_class TEXT, p8_phone TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(username, event_id)
	);`

	if _, err := db.Exec(createUsrRegsTable); err != nil {
		return fmt.Errorf("error creating usr_regs table: %v", err)
	}

	if _, err := db.Exec(createIndexes); err != nil {
		return fmt.Errorf("error creating indexes: %v", err)
	}

	log.Println("Database tables initialized successfully")

	migration := `
UPDATE users SET individual = CASE
    WHEN lower(individual) IN ('true','1','t','yes') THEN 1
    ELSE 0
END
WHERE typeof(individual) = 'text';`

	if _, err := db.Exec(migration); err != nil {
		log.Printf("db.InitTables migration error: %v", err)
	} else {
		log.Println("db.InitTables: normalized legacy 'individual' values")
	}

	return nil

}

func (db *Database) Get(entity string, key string) (interface{}, error) {
	switch entity {
	case "users":
		query := `SELECT id, username, email, password_hash, school_code, fullname, phone_number, principals_email, individual, institution_name, address, principals_name, registrations, created_at, updated_at FROM users WHERE email = ?`
		user := &User{}
		var registrationsStr string
		var individualNull sql.NullBool
		err := db.QueryRow(query, key).Scan(
			&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.SchoolCode, &user.Fullname, &user.PhoneNumber, &user.PrincipalsEmail, &individualNull, &user.InstitutionName, &user.Address, &user.PrincipalsName, &registrationsStr, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if individualNull.Valid {
			user.Individual = individualNull.Bool
		} else {
			user.Individual = false
		}
		user.unmarshalRegistrations(registrationsStr)
		return user, nil

	case "events":
		query := `SELECT id, name, image, open_to_all, eligibility, participants, mode, 
			independent_registration, points, dates, description_long, description_short, created_at, updated_at 
			FROM events WHERE id = ?`
		event := &Event{}
		err := db.QueryRow(query, key).Scan(
			&event.ID, &event.Name, &event.Image, &event.OpenToAll, &event.Eligibility,
			&event.Participants, &event.Mode, &event.IndependentRegistration, &event.Points, &event.Dates,
			&event.DescriptionLong, &event.DescriptionShort, &event.CreatedAt, &event.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return event, nil

	case "registrations":
		query := `SELECT id, event_id, user_id, team_name, status, created_at, updated_at FROM registrations WHERE id = ?`
		reg := &Registration{}
		err := db.QueryRow(query, key).Scan(
			&reg.ID, &reg.EventID, &reg.UserID, &reg.TeamName, &reg.Status, &reg.CreatedAt, &reg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return reg, nil

	case "individual_registrations":
		query := `SELECT id, user_id, fullname, user_email, created_at, updated_at FROM individual_registrations WHERE id = ?`
		ir := &IndividualRegistration{}
		err := db.QueryRow(query, key).Scan(&ir.ID, &ir.UserID, &ir.Fullname, &ir.UserEmail, &ir.CreatedAt, &ir.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return ir, nil

	case "individual_registrations_by_user":
		uid, err := strconv.Atoi(key)
		if err != nil {
			return nil, fmt.Errorf("invalid user id: %v", err)
		}
		query := `SELECT id, user_id, fullname, user_email, created_at, updated_at FROM individual_registrations WHERE user_id = ? LIMIT 1`
		ir := &IndividualRegistration{}
		err = db.QueryRow(query, uid).Scan(&ir.ID, &ir.UserID, &ir.Fullname, &ir.UserEmail, &ir.CreatedAt, &ir.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return ir, nil

	default:
		return nil, fmt.Errorf("unknown entity: %s", entity)
	}
}

func (db *Database) Create(entity string, data interface{}) error {
	now := time.Now()

	switch entity {
	case "users":
		user, ok := data.(*User)
		if !ok {
			return fmt.Errorf("invalid user data")
		}
		query := `INSERT INTO users (username, email, password_hash, school_code, fullname, phone_number, principals_email, individual, institution_name, address, principals_name, registrations, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		indiv := 0
		if user.Individual {
			indiv = 1
		}
		_, err := db.Exec(query, user.Username, user.Email, user.PasswordHash, user.SchoolCode, user.Fullname, user.PhoneNumber, user.PrincipalsEmail, indiv, user.InstitutionName, user.Address, user.PrincipalsName, user.marshalRegistrations(), now, now)
		if err != nil {
			log.Printf("db.Create(users) error: %v", err)
		}
		return err

	case "events":
		event, ok := data.(*Event)
		if !ok {
			return fmt.Errorf("invalid event data")
		}
		query := `INSERT INTO events (id, name, image, open_to_all, eligibility, participants, mode, 
			independent_registration, points, dates, description_long, description_short, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		_, err := db.Exec(query, event.ID, event.Name, event.Image, event.OpenToAll, event.Eligibility,
			event.Participants, event.Mode, event.IndependentRegistration, event.Points, event.Dates,
			event.DescriptionLong, event.DescriptionShort, now, now)
		if err != nil {
			log.Printf("db.Create(events) error: %v", err)
		}
		return err

	case "registrations":
		reg, ok := data.(*Registration)
		if !ok {
			return fmt.Errorf("invalid registration data")
		}
		query := `INSERT INTO registrations (event_id, user_id, team_name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`
		_, err := db.Exec(query, reg.EventID, reg.UserID, reg.TeamName, reg.Status, now, now)
		if err != nil {
			log.Printf("db.Create(registrations) error: %v", err)
		}
		return err

	case "individual_registrations":
		ir, ok := data.(*IndividualRegistration)
		if !ok {
			return fmt.Errorf("invalid individual registration data")
		}
		query := `INSERT INTO individual_registrations (user_id, fullname, user_email, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
		_, err := db.Exec(query, ir.UserID, ir.Fullname, ir.UserEmail, now, now)
		if err != nil {
			log.Printf("db.Create(individual_registrations) error: %v", err)
		}
		return err

	case "logs":
		le, ok := data.(*LogEntry)
		if !ok {
			return fmt.Errorf("invalid log data")
		}
		query := `INSERT INTO logs (reason, content, created_at) VALUES (?, ?, ?)`
		_, err := db.Exec(query, le.Reason, le.Content, now)
		if err != nil {
			log.Printf("db.Create(logs) error: %v", err)
		}
		return err

	default:
		return fmt.Errorf("unknown entity: %s", entity)
	}
}

func (db *Database) Update(entity string, key string, data interface{}) error {
	now := time.Now()

	switch entity {
	case "users":
		user, ok := data.(*User)
		if !ok {
			return fmt.Errorf("invalid user data")
		}
		query := `UPDATE users SET username = ?, password_hash = ?, school_code = ?, fullname = ?, phone_number = ?, principals_email = ?, individual = ?, institution_name = ?, address = ?, principals_name = ?, registrations = ?, updated_at = ? WHERE email = ?`
		indiv := 0
		if user.Individual {
			indiv = 1
		}
		_, err := db.Exec(query, user.Username, user.PasswordHash, user.SchoolCode, user.Fullname, user.PhoneNumber, user.PrincipalsEmail, indiv, user.InstitutionName, user.Address, user.PrincipalsName, user.marshalRegistrations(), now, key)
		if err != nil {
			log.Printf("db.Update(users) error: %v", err)
		}
		return err

	case "events":
		event, ok := data.(*Event)
		if !ok {
			return fmt.Errorf("invalid event data")
		}
		query := `UPDATE events SET name = ?, image = ?, open_to_all = ?, eligibility = ?, participants = ?, 
			mode = ?, independent_registration = ?, points = ?, dates = ?, description_long = ?, 
			description_short = ?, updated_at = ? WHERE id = ?`
		_, err := db.Exec(query, event.Name, event.Image, event.OpenToAll, event.Eligibility,
			event.Participants, event.Mode, event.IndependentRegistration, event.Points, event.Dates,
			event.DescriptionLong, event.DescriptionShort, now, key)
		if err != nil {
			log.Printf("db.Update(events) error: %v", err)
		}
		return err

	case "registrations":
		reg, ok := data.(*Registration)
		if !ok {
			return fmt.Errorf("invalid registration data")
		}
		query := `UPDATE registrations SET event_id = ?, user_id = ?, team_name = ?, status = ?, updated_at = ? WHERE id = ?`
		_, err := db.Exec(query, reg.EventID, reg.UserID, reg.TeamName, reg.Status, now, key)
		if err != nil {
			log.Printf("db.Update(registrations) error: %v", err)
		}
		return err

	case "individual_registrations":
		ir, ok := data.(*IndividualRegistration)
		if !ok {
			return fmt.Errorf("invalid individual registration data")
		}
		query := `UPDATE individual_registrations SET user_id = ?, fullname = ?, user_email = ?, updated_at = ? WHERE id = ?`
		_, err := db.Exec(query, ir.UserID, ir.Fullname, ir.UserEmail, now, key)
		if err != nil {
			log.Printf("db.Update(individual_registrations) error: %v", err)
		}
		return err

	default:
		return fmt.Errorf("unknown entity: %s", entity)
	}
}

func (db *Database) Delete(entity string, key string) error {
	switch entity {
	case "users":
		query := `DELETE FROM users WHERE email = ?`
		_, err := db.Exec(query, key)
		return err

	case "events":
		query := `DELETE FROM events WHERE id = ?`
		_, err := db.Exec(query, key)
		return err

	case "registrations":
		query := `DELETE FROM registrations WHERE id = ?`
		_, err := db.Exec(query, key)
		return err

	case "individual_registrations":
		query := `DELETE FROM individual_registrations WHERE id = ?`
		_, err := db.Exec(query, key)
		return err

	case "individual_registrations_by_user":
		uid, err := strconv.Atoi(key)
		if err != nil {
			return fmt.Errorf("invalid user id: %v", err)
		}
		query := `DELETE FROM individual_registrations WHERE user_id = ?`
		_, err = db.Exec(query, uid)
		return err

	default:
		return fmt.Errorf("unknown entity: %s", entity)
	}
}

func (db *Database) GetAll(entity string) ([]interface{}, error) {
	switch entity {
	case "users":
		query := `SELECT id, username, email, password_hash, school_code, fullname, phone_number, principals_email, individual, institution_name, address, principals_name, registrations, created_at, updated_at FROM users`
		rows, err := db.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var users []interface{}
		for rows.Next() {
			user := &User{}
			var registrationsStr string
			var individualNull sql.NullBool
			err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.SchoolCode, &user.Fullname, &user.PhoneNumber, &user.PrincipalsEmail, &individualNull, &user.InstitutionName, &user.Address, &user.PrincipalsName, &registrationsStr, &user.CreatedAt, &user.UpdatedAt)
			if err != nil {
				return nil, err
			}
			if individualNull.Valid {
				user.Individual = individualNull.Bool
			} else {
				user.Individual = false
			}
			user.unmarshalRegistrations(registrationsStr)
			users = append(users, user)
		}
		return users, nil

	case "events":
		query := `SELECT id, name, image, open_to_all, eligibility, participants, mode, 
			independent_registration, points, dates, description_long, description_short, created_at, updated_at FROM events`
		rows, err := db.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var events []interface{}
		for rows.Next() {
			event := &Event{}
			err := rows.Scan(&event.ID, &event.Name, &event.Image, &event.OpenToAll, &event.Eligibility,
				&event.Participants, &event.Mode, &event.IndependentRegistration, &event.Points, &event.Dates,
				&event.DescriptionLong, &event.DescriptionShort, &event.CreatedAt, &event.UpdatedAt)
			if err != nil {
				return nil, err
			}
			events = append(events, event)
		}
		return events, nil

	case "registrations":
		query := `SELECT id, event_id, user_id, team_name, status, created_at, updated_at FROM registrations`
		rows, err := db.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var registrations []interface{}
		for rows.Next() {
			reg := &Registration{}
			err := rows.Scan(&reg.ID, &reg.EventID, &reg.UserID, &reg.TeamName, &reg.Status, &reg.CreatedAt, &reg.UpdatedAt)
			if err != nil {
				return nil, err
			}
			registrations = append(registrations, reg)
		}
		return registrations, nil

	case "individual_registrations":
		query := `SELECT id, user_id, fullname, user_email, created_at, updated_at FROM individual_registrations`
		rows, err := db.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var irs []interface{}
		for rows.Next() {
			ir := &IndividualRegistration{}
			err := rows.Scan(&ir.ID, &ir.UserID, &ir.Fullname, &ir.UserEmail, &ir.CreatedAt, &ir.UpdatedAt)
			if err != nil {
				return nil, err
			}
			irs = append(irs, ir)
		}
		return irs, nil

	case "logs":
		query := `SELECT id, reason, content, created_at FROM logs ORDER BY created_at DESC`
		rows, err := db.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var logs []interface{}
		for rows.Next() {
			le := &LogEntry{}
			var createdAtStr string
			if err := rows.Scan(&le.ID, &le.Reason, &le.Content, &createdAtStr); err != nil {
				return nil, err
			}
			t, err := time.Parse("2006-01-02 15:04:05", createdAtStr)
			if err != nil {
				le.CreatedAt = time.Now()
			} else {
				le.CreatedAt = t
			}
			logs = append(logs, le)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return logs, nil

	default:
		return nil, fmt.Errorf("unknown entity: %s", entity)
	}
}
