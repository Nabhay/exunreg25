package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"exunreg25/db"

	"google.golang.org/api/option"
	sheets "google.golang.org/api/sheets/v4"
)

// Event definition for building users sheet registration columns
type eventDef struct {
	Slug         string
	Participants int
}

func getEventDefs() ([]eventDef, error) {
	evs, err := GetAllEventsData()
	if err != nil {
		return nil, err
	}
	out := make([]eventDef, 0, len(evs))
	for _, e := range evs {
		out = append(out, eventDef{Slug: e.ID, Participants: e.Participants})
	}
	return out, nil
}

// Build desired headers for the users sheet: DB columns + flattened registration columns per event
func buildUsersHeaders(dbHeader []interface{}, evs []eventDef) []string {
	headers := make([]string, 0, len(dbHeader))
	for _, c := range dbHeader {
		headers = append(headers, fmt.Sprintf("%v", c))
	}
	for _, e := range evs {
		p := e.Participants
		if p <= 0 {
			p = 1
		}
		for i := 1; i <= p; i++ {
			base := fmt.Sprintf("%s-%d", e.Slug, i)
			headers = append(headers, base+" name")
			headers = append(headers, base+" email")
			headers = append(headers, base+" number")
			headers = append(headers, base+" class")
		}
	}
	return headers
}

// Parse a flattened registration header into (slug, index [1-based], field)
func parseRegHeader(h string) (slug string, idx int, field string, ok bool) {
	h = strings.TrimSpace(strings.ToLower(h))
	// split on last space to get field
	sp := strings.LastIndex(h, " ")
	if sp <= 0 || sp >= len(h)-1 {
		return
	}
	left := h[:sp]
	field = strings.TrimSpace(h[sp+1:])
	if field != "name" && field != "email" && field != "number" && field != "class" {
		return
	}
	dash := strings.LastIndex(left, "-")
	if dash <= 0 || dash >= len(left)-1 {
		return
	}
	slug = left[:dash]
	nstr := left[dash+1:]
	n, err := strconv.Atoi(nstr)
	if err != nil || n <= 0 {
		return
	}
	idx = n
	ok = true
	return
}

// Extract value for a sheet header from a DB users row (handles flattened registration columns)
func getUsersValueForHeader(dbIndexLower map[string]int, dbRow []interface{}, header string) string {
	if di, ok := dbIndexLower[strings.ToLower(header)]; ok {
		if di >= 0 && di < len(dbRow) {
			return fmt.Sprintf("%v", dbRow[di])
		}
		return ""
	}
	// Try flattened registration column
	slug, idx, field, ok := parseRegHeader(header)
	if !ok {
		return ""
	}
	regsIdx, hasRegs := dbIndexLower["registrations"]
	if !hasRegs || regsIdx < 0 || regsIdx >= len(dbRow) {
		return ""
	}
	raw := fmt.Sprintf("%v", dbRow[regsIdx])
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return ""
	}
	var regs map[string][]db.Participant
	if err := json.Unmarshal([]byte(raw), &regs); err != nil {
		return ""
	}
	parts := regs[slug]
	pos := idx - 1
	if pos < 0 || pos >= len(parts) {
		return ""
	}
	pr := parts[pos]
	switch field {
	case "name":
		return pr.Name
	case "email":
		return pr.Email
	case "number":
		return pr.Phone
	case "class":
		if pr.Class == 0 {
			return ""
		}
		return fmt.Sprintf("%d", pr.Class)
	default:
		return ""
	}
}

// Assemble registrations JSON from a sheet row using flattened columns; returns JSON string
func buildRegistrationsFromSheetRow(headers []string, row []string) string {
	byEvent := map[string]map[int]db.Participant{}
	for i, h := range headers {
		if i >= len(row) {
			continue
		}
		slug, idx, field, ok := parseRegHeader(h)
		if !ok {
			continue
		}
		if _, ok := byEvent[slug]; !ok {
			byEvent[slug] = map[int]db.Participant{}
		}
		val := strings.TrimSpace(row[i])
		p := byEvent[slug][idx]
		switch field {
		case "name":
			p.Name = val
		case "email":
			p.Email = val
		case "number":
			p.Phone = val
		case "class":
			if val == "" {
				p.Class = 0
			} else {
				if n, err := strconv.Atoi(val); err == nil {
					p.Class = n
				} else {
					p.Class = 0
				}
			}
		}
		byEvent[slug][idx] = p
	}
	out := map[string][]db.Participant{}
	for slug, mp := range byEvent {
		// Compact indices starting at 1, keep those with any non-empty field
		maxIdx := 0
		for k := range mp {
			if k > maxIdx {
				maxIdx = k
			}
		}
		arr := make([]db.Participant, 0, maxIdx)
		for i := 1; i <= maxIdx; i++ {
			p := mp[i]
			if strings.TrimSpace(p.Name) == "" && strings.TrimSpace(p.Email) == "" && strings.TrimSpace(p.Phone) == "" {
				continue
			}
			// Preserve class captured from the sheet instead of resetting to 0
			arr = append(arr, db.Participant{Name: p.Name, Email: p.Email, Class: p.Class, Phone: p.Phone})
		}
		if len(arr) > 0 {
			out[slug] = arr
		}
	}
	if len(out) == 0 {
		return "{}"
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func parseFlexibleTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}

	tryParse := func(str string) (time.Time, error) {
		layouts := []string{
			"2006-01-02 15:04:05.999999999 -0700 MST",
			"2006-01-02 15:04:05.999999 -0700 MST",
			"2006-01-02 15:04:05.999 -0700 MST",
			"2006-01-02 15:04:05 -0700 MST",
			"2006-01-02 15:04:05.999999999 -0700",
			"2006-01-02 15:04:05.999999 -0700",
			"2006-01-02 15:04:05.999 -0700",
			"2006-01-02 15:04:05 -0700",
			"2006-01-02T15:04:05.999999999-0700",
			"2006-01-02T15:04:05.999999-0700",
			"2006-01-02T15:04:05-0700",
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05",
		}
		for _, l := range layouts {
			if t, err := time.ParseInLocation(l, str, time.UTC); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("unrecognized time format: %s", str)
	}

	if t, err := tryParse(s); err == nil {
		return t, nil
	}

	parts := strings.Fields(s)
	if len(parts) >= 2 {
		last := parts[len(parts)-1]
		secondLast := parts[len(parts)-2]
		if last == secondLast {
			ns := strings.Join(parts[:len(parts)-1], " ")
			if t, err := tryParse(ns); err == nil {
				return t, nil
			}
		}
		if len(parts) >= 3 {
			p2 := parts[len(parts)-2]
			p1 := parts[len(parts)-3]
			if (strings.HasPrefix(p2, "+") || strings.HasPrefix(p2, "-")) && p1 == p2 {
				ns := strings.Join(parts[:len(parts)-2], " ") + " " + p2
				if t, err := tryParse(ns); err == nil {
					return t, nil
				}
			}
		}
	}

	if idx := strings.Index(s, " "); idx != -1 {
		s2 := s[:idx] + "T" + s[idx+1:]
		if t, err := tryParse(s2); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized time format: %s", s)
}

var (
	sheetsResetCh chan struct{}
	sheetsOpMu    sync.Mutex
)

func startSheetsSync(database *db.Database) {
	if sheetsResetCh == nil {
		sheetsResetCh = make(chan struct{}, 1)
	}
	interval := 100 * time.Second
	timer := time.NewTimer(interval)
	defer timer.Stop()

	backupInterval := 360 * time.Minute
	if v := os.Getenv("DRIVE_BACKUP_INTERVAL"); v != "" {
		if m, err := strconv.Atoi(v); err == nil && m > 0 {
			backupInterval = time.Duration(m) * time.Minute
		}
	}
	backupTicker := time.NewTicker(backupInterval)
	defer backupTicker.Stop()

	if err := syncAllTablesToSheets(database); err != nil {
		log.Printf("sheets sync initial run error: %v", err)
	}

	for {
		select {
		case <-timer.C:
			sheetsOpMu.Lock()
			log.Printf("starting sheets sync")
			if err := syncAllTablesToSheets(database); err != nil {
				log.Printf("sheets sync error: %v", err)
			} else {
				log.Printf("sheets sync completed")
			}
			sheetsOpMu.Unlock()
			timer.Reset(interval)
		case <-sheetsResetCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			sheetsOpMu.Lock()
			log.Printf("starting sheets sync (manual trigger)")
			if err := syncAllTablesToSheets(database); err != nil {
				log.Printf("sheets sync error: %v", err)
			} else {
				log.Printf("sheets sync completed (manual trigger)")
			}
			sheetsOpMu.Unlock()
			timer.Reset(interval)
		case <-backupTicker.C:
			sheetsOpMu.Lock()
			saJSON := os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON")
			if saJSON != "" {
				if _, err := os.Stat(saJSON); err == nil {
					b, err := os.ReadFile(saJSON)
					if err == nil {
						dbPath := os.Getenv("DB_PATH")
						if dbPath == "" {
							dbPath = "./data/exunreg25.db"
						}
						log.Printf("scheduled Drive backup for %s", dbPath)
						id, err := UploadDBBackupToDrive(b, dbPath)
						if err != nil {
							log.Printf("drive backup error: %v", err)
						} else {
							log.Printf("drive backup uploaded id: %s", id)
						}
					}
				}
			}
			sheetsOpMu.Unlock()
		}
	}
}

func syncAllTablesToSheets(database *db.Database) error {
	saJSON := os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON")
	if saJSON == "" {
		return fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_JSON not set")
	}
	if _, err := os.Stat(saJSON); err == nil {
		b, err := os.ReadFile(saJSON)
		if err != nil {
			return fmt.Errorf("failed to read service account file: %v", err)
		}
		saJSON = string(b)
	}
	spreadsheetID := os.Getenv("SPREADSHEET_ID")
	if spreadsheetID == "" {
		return fmt.Errorf("SPREADSHEET_ID not set")
	}

	ctx := context.Background()
	creds := []byte(saJSON)
	srv, err := sheets.NewService(ctx, option.WithCredentialsJSON(creds))
	if err != nil {
		return fmt.Errorf("failed to create sheets service: %v", err)
	}

	tables, err := listTables(database)
	if err != nil {
		return fmt.Errorf("failed to list tables: %v", err)
	}

	// NOTE: usr_regs syncing is removed. All registrations are managed via the users sheet only.

	// Only sync the users table with Sheets
	primaryKeys := map[string]string{
		"users": "email",
	}

	// Restrict tables to users only
	tables = []string{"users"}

	for _, t := range tables {
		rows, err := queryTableRows(database, t)
		if err != nil {
			log.Printf("failed to query table %s: %v", t, err)
			continue
		}

		sheetName := t
		sheetId, err := ensureSheetExists(ctx, srv, spreadsheetID, sheetName)
		if err != nil {
			log.Printf("failed to ensure sheet %s exists: %v", sheetName, err)
			continue
		}

		hdr, sheetRows, err := readSheetRows(ctx, srv, spreadsheetID, sheetName)
		if err != nil {
			log.Printf("failed to read sheet %s: %v", sheetName, err)
			if t == "users" {
				// Initialize users sheet with custom headers and data
				evs, _ := getEventDefs()
				if len(rows) == 0 {
					log.Printf("no DB rows for users to initialize")
					continue
				}
				dbHeader := rows[0]
				desired := buildUsersHeaders(dbHeader, evs)
				// Build values
				dbIndexLower := map[string]int{}
				for i, c := range dbHeader {
					dbIndexLower[strings.ToLower(fmt.Sprintf("%v", c))] = i
				}
				values := make([][]interface{}, 0, len(rows))
				// header row
				hdrRow := make([]interface{}, len(desired))
				for i, h := range desired {
					hdrRow[i] = h
				}
				values = append(values, hdrRow)
				for _, dbRow := range rows[1:] {
					line := make([]interface{}, len(desired))
					for ci, h := range desired {
						line[ci] = getUsersValueForHeader(dbIndexLower, dbRow, h)
					}
					values = append(values, line)
				}
				rangeA1 := sheetName + "!A1"
				vr := &sheets.ValueRange{Values: values}
				if _, err := srv.Spreadsheets.Values.Update(spreadsheetID, rangeA1, vr).ValueInputOption("RAW").Do(); err != nil {
					log.Printf("failed to initialize users sheet: %v", err)
				}
				continue
			} else {
				vr := &sheets.ValueRange{Values: convertToValues(rows)}
				rangeA1 := sheetName + "!A1"
				_, err = srv.Spreadsheets.Values.Update(spreadsheetID, rangeA1, vr).ValueInputOption("RAW").Do()
				if err != nil {
					log.Printf("failed to update sheet %s: %v", sheetName, err)
				}
				continue
			}
		}

		// For users sheet, ensure desired headers exist; if not, rewrite header row
		if t == "users" && len(rows) > 0 {
			evs, _ := getEventDefs()
			desired := buildUsersHeaders(rows[0], evs)
			needRewrite := len(hdr) != len(desired)
			if !needRewrite {
				for i := range hdr {
					if i >= len(desired) || !strings.EqualFold(hdr[i], desired[i]) {
						needRewrite = true
						break
					}
				}
			}
			if needRewrite {
				rng := sheetName + "!A1"
				row := make([]interface{}, len(desired))
				for i, h := range desired {
					row[i] = h
				}
				if _, err := srv.Spreadsheets.Values.Update(spreadsheetID, rng, &sheets.ValueRange{Values: [][]interface{}{row}}).ValueInputOption("RAW").Do(); err != nil {
					log.Printf("failed to rewrite users header: %v", err)
				} else {
					hdr = desired
				}
			} else {
				hdr = desired
			}
		}

		pk, ok := primaryKeys[t]
		if ok {
			pkIndex := -1
			idIndex := -1
			sheetUpdatedIndex := -1
			for i, h := range hdr {
				if strings.EqualFold(h, pk) {
					pkIndex = i
				}
				if strings.EqualFold(h, "id") {
					idIndex = i
				}
				if strings.EqualFold(h, "updated_at") {
					sheetUpdatedIndex = i
				}
			}
			if len(rows) > 0 {
				dbHeader := rows[0]
				dbIndex := map[string]int{}
				for i, c := range dbHeader {
					dbIndex[fmt.Sprintf("%v", c)] = i
				}
				dbUpdatedIdx := -1
				if v, ok := dbIndex["updated_at"]; ok {
					dbUpdatedIdx = v
				}
				dbPkIdx := -1
				if v, ok := dbIndex[pk]; ok {
					dbPkIdx = v
				}
				// Important: avoid pushing DB->Sheet before applying sheet edits for users.
				if !strings.EqualFold(t, "users") {
					for sr, r := range sheetRows {
						var pkVal string
						if pkIndex >= 0 && pkIndex < len(r) {
							pkVal = strings.TrimSpace(r[pkIndex])
							if pkVal == "''" {
								pkVal = ""
							}
						}
						if pkVal == "" {
							continue
						}
						var sheetUpdated time.Time
						if sheetUpdatedIndex >= 0 && sheetUpdatedIndex < len(r) {
							s := strings.TrimSpace(r[sheetUpdatedIndex])
							if s == "''" {
								s = ""
							}
							ts, _ := parseFlexibleTime(s)
							sheetUpdated = ts
							log.Printf("sheet->db: table=%s pk=%s rawSheetUpdated=%q parsedSheetUpdated=%v", t, pkVal, s, sheetUpdated)
						}
						var matchedDBRow []interface{}
						for _, dbRow := range rows[1:] {
							if dbPkIdx >= 0 && dbPkIdx < len(dbRow) {
								v := fmt.Sprintf("%v", dbRow[dbPkIdx])
								if v == pkVal {
									matchedDBRow = dbRow
									break
								}
							}
						}
						if matchedDBRow == nil {
							continue
						}
						var dbUpdated time.Time
						if dbUpdatedIdx >= 0 && dbUpdatedIdx < len(matchedDBRow) {
							rawDB := fmt.Sprintf("%v", matchedDBRow[dbUpdatedIdx])
							tdb, _ := parseFlexibleTime(rawDB)
							dbUpdated = tdb
							log.Printf("sheet->db: table=%s pk=%s rawDBUpdated=%q parsedDBUpdated=%v", t, pkVal, rawDB, dbUpdated)
						}
						if dbUpdated.IsZero() {
							log.Printf("sheet->db: table=%s pk=%s dbUpdated is zero, skipping comparison", t, pkVal)
							continue
						}
						if dbUpdated.After(sheetUpdated) {
							colsCount := len(hdr)
							rowNum := sr + 2
							for ci := 0; ci < colsCount; ci++ {
								h := hdr[ci]
								var dbVal string
								if di, ok := dbIndex[h]; ok && di >= 0 && di < len(matchedDBRow) {
									dbVal = fmt.Sprintf("%v", matchedDBRow[di])
								} else {
									dbVal = ""
								}
								var sheetVal string
								if ci < len(r) {
									sheetVal = r[ci]
								} else {
									sheetVal = ""
								}
								if dbVal != sheetVal {
									col := a1ColumnName(ci)
									rng := fmt.Sprintf("%s!%s%d", sheetName, col, rowNum)
									vr := &sheets.ValueRange{Range: rng, Values: [][]interface{}{{dbVal}}}
									res, err := srv.Spreadsheets.Values.Update(spreadsheetID, rng, vr).ValueInputOption("RAW").Context(ctx).Do()
									if err != nil {
										log.Printf("failed to update sheet %s for pk=%s (cell=%s%d): %v", sheetName, pkVal, col, rowNum, err)
									} else {
										log.Printf("updated sheet %s for pk=%s (cell=%s%d): updatedCells=%d", sheetName, pkVal, col, rowNum, res.UpdatedCells)
									}
								}
							}
						}
					}
				}
			}
			sheetPKs := make(map[string]bool)
			if pkIndex >= 0 || idIndex >= 0 {
				seenPK := map[string]int{}
				seenID := map[string]int{}
				toDeleteSet := map[int]bool{}
				dbDeleteIDs := []string{}
				for i, r := range sheetRows {
					var pkVal string
					if pkIndex >= 0 && pkIndex < len(r) {
						pkVal = strings.TrimSpace(r[pkIndex])
						if pkVal == "''" {
							pkVal = ""
						}
					}
					var idVal string
					if idIndex >= 0 && idIndex < len(r) {
						idVal = strings.TrimSpace(r[idIndex])
						if idVal == "''" {
							idVal = ""
						}
					}
					if pkVal != "" {
						if first, ok := seenPK[pkVal]; ok {
							toDeleteSet[i] = true
							_ = first
							continue
						}
						seenPK[pkVal] = i
					}
					if idVal != "" {
						if first, ok := seenID[idVal]; ok {
							toDeleteSet[i] = true
							_ = first
							continue
						}
						seenID[idVal] = i
					}
					if idVal != "" && pkVal == "" {
						allEmpty := true
						for j, cell := range r {
							if j == idIndex {
								continue
							}
							c := strings.TrimSpace(cell)
							if c == "''" {
								c = ""
							}
							if c != "" {
								allEmpty = false
								break
							}
						}
						if allEmpty {
							toDeleteSet[i] = true
							dbDeleteIDs = append(dbDeleteIDs, idVal)
						}
					}
				}
				if len(toDeleteSet) > 0 {
					idxs := make([]int, 0, len(toDeleteSet))
					for k := range toDeleteSet {
						idxs = append(idxs, k)
					}
					sort.Slice(idxs, func(a, b int) bool { return idxs[a] > idxs[b] })
					requests := []*sheets.Request{}
					for _, sr := range idxs {
						startIndex := int64(sr + 1)
						endIndex := startIndex + 1
						deleteReq := &sheets.DeleteDimensionRequest{
							Range: &sheets.DimensionRange{
								SheetId:    sheetId,
								Dimension:  "ROWS",
								StartIndex: startIndex,
								EndIndex:   endIndex,
							},
						}
						requests = append(requests, &sheets.Request{DeleteDimension: deleteReq})
					}
					if len(requests) > 0 {
						batch := &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}
						_, _ = srv.Spreadsheets.BatchUpdate(spreadsheetID, batch).Context(ctx).Do()
					}
					if len(dbDeleteIDs) > 0 {
						for _, did := range dbDeleteIDs {
							dq := fmt.Sprintf("DELETE FROM %s WHERE id = ?", t)
							_, _ = database.Exec(dq, did)
						}
					}
					newRows := make([][]string, 0, len(sheetRows)-len(toDeleteSet))
					for i, r := range sheetRows {
						if _, del := toDeleteSet[i]; del {
							continue
						}
						newRows = append(newRows, r)
					}
					sheetRows = newRows
				}
				for _, r := range sheetRows {
					if pkIndex >= 0 && pkIndex < len(r) {
						v := strings.TrimSpace(r[pkIndex])
						if v == "''" {
							v = ""
						}
						if v != "" {
							sheetPKs[v] = true
						}
					}
				}
			}
			// Do not delete DB rows based on sheet contents; allow adding users in the sheet.
			skippedPKs := []string{}
			if err := applyTableUpserts(database, t, pk, hdr, sheetRows, &skippedPKs); err != nil {
				log.Printf("failed to apply upserts for table %s: %v", t, err)
			}
			rows, err = queryTableRows(database, t)
			if err != nil {
				log.Printf("failed to re-query table %s: %v", t, err)
				continue
			}
			if len(rows) > 0 {
				dbHeader := rows[0]
				dbIndex := map[string]int{}
				for i, c := range dbHeader {
					dbIndex[fmt.Sprintf("%v", c)] = i
				}
				dbUpdatedIdx := -1
				if v, ok := dbIndex["updated_at"]; ok {
					dbUpdatedIdx = v
				}
				dbPkIdx := -1
				if v, ok := dbIndex[pk]; ok {
					dbPkIdx = v
				}
				colsCount := len(hdr)
				sheetMap := map[string]int{}
				for i, r := range sheetRows {
					if pkIndex >= 0 && pkIndex < len(r) {
						sheetMap[strings.TrimSpace(r[pkIndex])] = i
					}
				}
				skippedSet := map[string]bool{}
				for _, s := range skippedPKs {
					skippedSet[s] = true
				}
				// Batch containers to reduce Sheets write requests
				rowUpdates := []*sheets.ValueRange{}
				rowsToAppend := [][]interface{}{}
				for _, dbRow := range rows[1:] {
					if dbPkIdx < 0 || dbPkIdx >= len(dbRow) {
						continue
					}
					pkVal := fmt.Sprintf("%v", dbRow[dbPkIdx])
					if pkVal == "" {
						continue
					}
					dbUpdated := time.Time{}
					rawDBUpdated := ""
					if dbUpdatedIdx >= 0 && dbUpdatedIdx < len(dbRow) {
						rawDBUpdated = fmt.Sprintf("%v", dbRow[dbUpdatedIdx])
						dbUpdated, _ = parseFlexibleTime(rawDBUpdated)
					}
					sr, exists := sheetMap[pkVal]
					var sheetUpdated time.Time
					if exists {
						if sheetUpdatedIndex >= 0 && sheetUpdatedIndex < len(sheetRows[sr]) {
							s := strings.TrimSpace(sheetRows[sr][sheetUpdatedIndex])
							if s == "''" {
								s = ""
							}
							sheetUpdated, _ = parseFlexibleTime(s)
						}
					}
					if dbUpdated.IsZero() {
						log.Printf("db->sheet: table=%s pk=%s rawDBUpdated=%q parsedZero, skipping", t, pkVal, rawDBUpdated)
						continue
					}
					if !exists {
						newRow := make([]interface{}, colsCount)
						// Build lower-cased DB index map once
						dbIndexLower := map[string]int{}
						for k, v := range dbIndex {
							dbIndexLower[strings.ToLower(k)] = v
						}
						for ci := 0; ci < colsCount; ci++ {
							h := hdr[ci]
							if t == "users" {
								newRow[ci] = getUsersValueForHeader(dbIndexLower, dbRow, h)
							} else if di, ok := dbIndex[h]; ok && di >= 0 && di < len(dbRow) {
								newRow[ci] = fmt.Sprintf("%v", dbRow[di])
							} else {
								newRow[ci] = ""
							}
						}
						rowsToAppend = append(rowsToAppend, newRow)
						continue
					}
					log.Printf("db->sheet: table=%s pk=%s dbUpdated=%v sheetUpdated=%v exists=%v skipped=%v", t, pkVal, dbUpdated, sheetUpdated, exists, skippedSet[pkVal])
					if dbUpdated.After(sheetUpdated) || skippedSet[pkVal] {
						// Build full row from DB and queue a single row-range update
						dbIndexLower := map[string]int{}
						for k, v := range dbIndex {
							dbIndexLower[strings.ToLower(k)] = v
						}
						rowVals := make([]interface{}, colsCount)
						for ci := 0; ci < colsCount; ci++ {
							h := hdr[ci]
							if t == "users" {
								rowVals[ci] = getUsersValueForHeader(dbIndexLower, dbRow, h)
							} else if di, ok := dbIndex[h]; ok && di >= 0 && di < len(dbRow) {
								rowVals[ci] = fmt.Sprintf("%v", dbRow[di])
							} else {
								rowVals[ci] = ""
							}
						}
						lastCol := a1ColumnName(colsCount - 1)
						rowNum := sr + 2
						rng := fmt.Sprintf("%s!A%d:%s%d", sheetName, rowNum, lastCol, rowNum)
						rowUpdates = append(rowUpdates, &sheets.ValueRange{Range: rng, Values: [][]interface{}{rowVals}})
					}
				}
				// Execute batched updates (one request)
				if len(rowUpdates) > 0 {
					_, err := srv.Spreadsheets.Values.BatchUpdate(spreadsheetID, &sheets.BatchUpdateValuesRequest{
						ValueInputOption: "RAW",
						Data:             rowUpdates,
					}).Context(ctx).Do()
					if err != nil {
						log.Printf("batch row updates failed for sheet %s: %v", sheetName, err)
					}
				}
				if len(rowsToAppend) > 0 {
					_, err := srv.Spreadsheets.Values.Append(spreadsheetID, sheetName+"!A1", &sheets.ValueRange{Values: rowsToAppend}).ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
					if err != nil {
						log.Printf("batch append failed for sheet %s: %v", sheetName, err)
					}
				}
			}
		}

		pkForUpdate := ""
		if p, ok := primaryKeys[t]; ok {
			pkForUpdate = p
		}
		if err := updateOnlyMissingCells(ctx, srv, spreadsheetID, sheetName, sheetId, pkForUpdate, hdr, sheetRows, rows); err != nil {
			log.Printf("failed to partially update sheet %s: %v", sheetName, err)
			continue
		}

		if len(rows) > 0 {
			colsCount := len(rows[0])
			requests := []*sheets.Request{}
			updateGrid := &sheets.UpdateSheetPropertiesRequest{
				Properties: &sheets.SheetProperties{
					SheetId: sheetId,
					GridProperties: &sheets.GridProperties{
						FrozenRowCount: 1,
					},
				},
				Fields: "gridProperties.frozenRowCount",
			}
			requests = append(requests, &sheets.Request{UpdateSheetProperties: updateGrid})

			repeat := &sheets.Request{RepeatCell: &sheets.RepeatCellRequest{
				Range: &sheets.GridRange{
					SheetId:          sheetId,
					StartRowIndex:    0,
					EndRowIndex:      1,
					StartColumnIndex: 0,
					EndColumnIndex:   int64(colsCount),
				},
				Cell:   &sheets.CellData{UserEnteredFormat: &sheets.CellFormat{TextFormat: &sheets.TextFormat{Bold: true}}},
				Fields: "userEnteredFormat.textFormat.bold",
			}}
			requests = append(requests, repeat)
			batch := &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}
			_, _ = srv.Spreadsheets.BatchUpdate(spreadsheetID, batch).Context(ctx).Do()
		}
	}

	// usr_regs->users merge removed; users sheet is the single source for registrations editing.
	return nil
}

func mergeUsrRegsToUsers(database *db.Database) error {
	q := `SELECT username, event_id, p1_name, p1_email, p1_class, p1_phone, p2_name, p2_email, p2_class, p2_phone, p3_name, p3_email, p3_class, p3_phone, p4_name, p4_email, p4_class, p4_phone, p5_name, p5_email, p5_class, p5_phone, p6_name, p6_email, p6_class, p6_phone, p7_name, p7_email, p7_class, p7_phone, p8_name, p8_email, p8_class, p8_phone, updated_at FROM usr_regs`
	rows, err := database.Query(q)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type userRegsData struct {
		regs    map[string][]db.Participant
		updated time.Time
	}

	usersMap := map[string]*userRegsData{}

	for rows.Next() {
		var username, eventID string
		var p [8]struct{ name, email, class, phone sql.NullString }
		var updated sql.NullString
		scanArgs := []interface{}{&username, &eventID}
		for i := 0; i < 8; i++ {
			scanArgs = append(scanArgs, &p[i].name, &p[i].email, &p[i].class, &p[i].phone)
		}
		scanArgs = append(scanArgs, &updated)
		if err := rows.Scan(scanArgs...); err != nil {
			continue
		}
		if username == "" {
			continue
		}
		parts := []db.Participant{}
		for i := 0; i < 8; i++ {
			if (p[i].name.Valid && strings.TrimSpace(p[i].name.String) != "") || (p[i].email.Valid && strings.TrimSpace(p[i].email.String) != "") {
				classInt := 0
				if p[i].class.Valid {
					if v, err := strconv.Atoi(strings.TrimSpace(p[i].class.String)); err == nil {
						classInt = v
					}
				}
				name := ""
				if p[i].name.Valid {
					name = p[i].name.String
				}
				email := ""
				if p[i].email.Valid {
					email = p[i].email.String
				}
				phone := ""
				if p[i].phone.Valid {
					phone = p[i].phone.String
				}
				parts = append(parts, db.Participant{Name: name, Email: email, Class: classInt, Phone: phone})
			}
		}
		ud := time.Time{}
		if updated.Valid {
			if t, err := parseFlexibleTime(strings.TrimSpace(updated.String)); err == nil {
				ud = t
			}
		}
		ur := usersMap[username]
		if ur == nil {
			ur = &userRegsData{regs: map[string][]db.Participant{}, updated: ud}
			usersMap[username] = ur
		}
		ur.regs[eventID] = parts
		if ud.After(ur.updated) {
			ur.updated = ud
		}
	}

	for username, ur := range usersMap {
		var regsStr sql.NullString
		var userUpdated sql.NullString
		err := database.QueryRow("SELECT registrations, updated_at FROM users WHERE username = ? LIMIT 1", username).Scan(&regsStr, &userUpdated)
		if err != nil {
			continue
		}
		var existing map[string][]db.Participant
		if regsStr.Valid && regsStr.String != "" && regsStr.String != "{}" {
			_ = json.Unmarshal([]byte(regsStr.String), &existing)
		} else {
			existing = map[string][]db.Participant{}
		}
		if reflect.DeepEqual(existing, ur.regs) {
			continue
		}
		userUpdT := time.Time{}
		if userUpdated.Valid {
			if t, err := parseFlexibleTime(strings.TrimSpace(userUpdated.String)); err == nil {
				userUpdT = t
			}
		}
		if ur.updated.Before(userUpdT) {
			continue
		}
		b, err := json.Marshal(ur.regs)
		if err != nil {
			continue
		}
		upd := time.Now()
		_, _ = database.Exec("UPDATE users SET registrations = ?, updated_at = ? WHERE username = ?", string(b), upd, username)
	}
	return nil
}

func listTables(database *db.Database) ([]string, error) {
	rows, err := database.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name;")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, nil
}

func queryTableRows(database *db.Database, table string) ([][]interface{}, error) {
	q := fmt.Sprintf("SELECT * FROM %s", table)
	rows, err := database.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := [][]interface{}{}
	header := make([]interface{}, len(cols))
	for i, c := range cols {
		header[i] = c
	}
	result = append(result, header)

	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		rec := make([]interface{}, len(cols))
		for i, v := range vals {
			switch val := v.(type) {
			case nil:
				rec[i] = ""
			case []byte:
				rec[i] = string(val)
			default:
				rec[i] = fmt.Sprintf("%v", val)
			}
		}
		result = append(result, rec)
	}
	return result, nil
}

func convertToValues(rows [][]interface{}) [][]interface{} {
	vals := make([][]interface{}, len(rows))
	for i, r := range rows {
		row := make([]interface{}, len(r))
		copy(row, r)
		vals[i] = row
	}
	return vals
}

func ensureSheetExists(ctx context.Context, srv *sheets.Service, spreadsheetID, sheetName string) (int64, error) {
	ss, err := srv.Spreadsheets.Get(spreadsheetID).Fields("sheets.properties").Do()
	if err != nil {
		return 0, err
	}
	for _, s := range ss.Sheets {
		if s.Properties.Title == sheetName {
			return s.Properties.SheetId, nil
		}
	}
	addReq := &sheets.Request{AddSheet: &sheets.AddSheetRequest{Properties: &sheets.SheetProperties{Title: sheetName}}}
	batch := &sheets.BatchUpdateSpreadsheetRequest{Requests: []*sheets.Request{addReq}}
	resp, err := srv.Spreadsheets.BatchUpdate(spreadsheetID, batch).Context(ctx).Do()
	if err != nil {
		return 0, err
	}
	if len(resp.Replies) > 0 && resp.Replies[0].AddSheet != nil && resp.Replies[0].AddSheet.Properties != nil {
		return resp.Replies[0].AddSheet.Properties.SheetId, nil
	}
	ss2, err := srv.Spreadsheets.Get(spreadsheetID).Fields("sheets.properties").Do()
	if err != nil {
		return 0, err
	}
	for _, s := range ss2.Sheets {
		if s.Properties.Title == sheetName {
			return s.Properties.SheetId, nil
		}
	}
	return 0, fmt.Errorf("failed to get sheet id for %s", sheetName)
}

func readSheetRows(ctx context.Context, srv *sheets.Service, spreadsheetID, sheetName string) ([]string, [][]string, error) {
	// Read a wide range to avoid truncating columns beyond Z
	rng := sheetName + "!A1:ZZZ"
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, rng).Context(ctx).Do()
	if err != nil {
		return nil, nil, err
	}
	if len(resp.Values) == 0 {
		return nil, nil, fmt.Errorf("empty sheet")
	}
	headerIface := resp.Values[0]
	headers := make([]string, len(headerIface))
	for i, h := range headerIface {
		headers[i] = fmt.Sprintf("%v", h)
	}
	rows := [][]string{}
	for _, r := range resp.Values[1:] {
		row := make([]string, len(headers))
		for i := range headers {
			if i < len(r) {
				row[i] = fmt.Sprintf("%v", r[i])
			} else {
				row[i] = ""
			}
		}
		rows = append(rows, row)
	}
	return headers, rows, nil
}

func applyTableUpserts(database *db.Database, table, pk string, headers []string, rows [][]string, skipped *[]string) error {
	if len(headers) == 0 {
		return fmt.Errorf("no headers")
	}
	dbRowsAll, _ := queryTableRows(database, table)
	dbIndex := map[string]int{}
	dbIndexLower := map[string]int{}
	dbPkIdx := -1
	if len(dbRowsAll) > 0 {
		for i, c := range dbRowsAll[0] {
			name := fmt.Sprintf("%v", c)
			dbIndex[name] = i
			dbIndexLower[strings.ToLower(name)] = i
		}
		if v, ok := dbIndex[pk]; ok {
			dbPkIdx = v
		}
	}
	updatedAtIndex := -1
	for i, h := range headers {
		if strings.EqualFold(h, "updated_at") {
			updatedAtIndex = i
			break
		}
	}
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	failed := false
	defer func() {
		if failed {
			_ = tx.Rollback()
			return
		}
		_ = tx.Commit()
	}()

	for _, r := range rows {
		values := map[string]string{}
		for i, h := range headers {
			var v string
			if i < len(r) {
				v = r[i]
			} else {
				v = ""
			}
			if strings.TrimSpace(v) == "''" {
				v = ""
			}
			values[h] = v
		}
		keyVal := values[pk]
		if keyVal == "" {
			continue
		}
		// Special handling for users: synthesize registrations JSON from flattened columns
		if strings.EqualFold(table, "users") {
			regsJSON := buildRegistrationsFromSheetRow(headers, r)
			values["registrations"] = regsJSON
		}
		if updatedAtIndex != -1 {
			q := fmt.Sprintf("SELECT updated_at FROM %s WHERE %s = ?", table, pk)
			var dbUpdated sql.NullString
			err := database.QueryRow(q, keyVal).Scan(&dbUpdated)
			if err == nil {
				sheetUpdatedStr := strings.TrimSpace(values[headers[updatedAtIndex]])
				if sheetUpdatedStr == "''" {
					sheetUpdatedStr = ""
				}
				sheetUpdated, _ := parseFlexibleTime(sheetUpdatedStr)
				dbUpdatedTime, _ := parseFlexibleTime(dbUpdated.String)
				if sheetUpdated.Before(dbUpdatedTime) {
					allow := false
					if len(dbRowsAll) > 1 && dbPkIdx >= 0 {
						var matchedDBRow []interface{}
						for _, dbr := range dbRowsAll[1:] {
							if dbPkIdx < len(dbr) {
								if fmt.Sprintf("%v", dbr[dbPkIdx]) == keyVal {
									matchedDBRow = dbr
									break
								}
							}
						}
						if matchedDBRow != nil {
							for _, h := range headers {
								if strings.EqualFold(h, pk) || strings.EqualFold(h, "updated_at") {
									continue
								}
								var dbCell string
								if di, ok := dbIndex[h]; ok && di >= 0 && di < len(matchedDBRow) {
									dbCell = fmt.Sprintf("%v", matchedDBRow[di])
								} else {
									dbCell = ""
								}
								sheetCell := values[h]
								if strings.TrimSpace(sheetCell) != "" && strings.TrimSpace(sheetCell) != strings.TrimSpace(dbCell) {
									allow = true
									break
								}
							}
						}
					}
					if !allow {
						log.Printf("apply upsert: table=%s key=%s sheetUpdated=%v dbUpdated=%v decision=skip", table, keyVal, sheetUpdated, dbUpdatedTime)
						if skipped != nil {
							*skipped = append(*skipped, keyVal)
						}
						continue
					}
				}
			}
		}
		cols := []string{}
		placeholders := []string{}
		args := []interface{}{}
		updates := []string{}
		for _, h := range headers {
			// Map sheet header to actual DB column (case-insensitive). Skip unknown headers (e.g., flattened reg columns).
			lower := strings.ToLower(h)
			dbColName := ""
			for name := range dbIndex {
				if strings.EqualFold(name, h) {
					dbColName = name
					break
				}
			}
			if dbColName == "" {
				// Not a DB column: skip
				continue
			}
			cols = append(cols, dbColName)
			placeholders = append(placeholders, "?")
			v := values[h]
			if strings.EqualFold(dbColName, "updated_at") {
				args = append(args, time.Now())
			} else {
				vtrim := strings.TrimSpace(v)
				if vtrim == "" {
					args = append(args, nil)
				} else {
					args = append(args, v)
				}
			}
			if !strings.EqualFold(dbColName, pk) {
				updates = append(updates, fmt.Sprintf("%s = COALESCE(excluded.%s, %s)", dbColName, dbColName, dbColName))
			}
			_ = lower
		}
		q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s", table, strings.Join(cols, ", "), strings.Join(placeholders, ", "), pk, strings.Join(updates, ", "))
		if _, execErr := tx.Exec(q, args...); execErr != nil {
			log.Printf("upsert error table %s key %s: %v", table, keyVal, execErr)
			failed = true
			return nil
		}
	}
	return nil
}

func deleteDBRowsNotInSheet(database *db.Database, table, pk string, sheetPKs map[string]bool) error {
	q := fmt.Sprintf("SELECT %s FROM %s", pk, table)
	rows, err := database.Query(q)
	if err != nil {
		return err
	}
	defer rows.Close()
	var toDelete []string
	for rows.Next() {
		var val sql.NullString
		if err := rows.Scan(&val); err != nil {
			return err
		}
		if !val.Valid {
			continue
		}
		s := val.String
		if _, ok := sheetPKs[s]; !ok {
			toDelete = append(toDelete, s)
		}
	}
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()
	for _, d := range toDelete {
		dq := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", table, pk)
		if _, err := tx.Exec(dq, d); err != nil {
			log.Printf("failed to delete %s=%s: %v", pk, d, err)
			continue
		}
	}
	return nil
}

func TriggerSheetsSync() error {
	if globalDB == nil {
		return fmt.Errorf("database not initialized")
	}
	if sheetsResetCh == nil {
		sheetsResetCh = make(chan struct{}, 1)
	}
	select {
	case sheetsResetCh <- struct{}{}:
	default:
	}
	return nil
}

func a1ColumnName(n int) string {
	name := ""
	for n >= 0 {
		ch := rune('A' + (n % 26))
		name = string([]rune{ch}) + name
		n = n/26 - 1
	}
	return name
}

// UpdateUsersSheetRowForEmail pushes the latest DB state for a single user to the users sheet immediately.
// DB is treated as the source of truth; the sheet row is inserted or fully overwritten.
func UpdateUsersSheetRowForEmail(database *db.Database, email string) error {
	if strings.TrimSpace(email) == "" {
		return fmt.Errorf("empty email")
	}
	saJSON := os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON")
	if saJSON == "" {
		return fmt.Errorf("GOOGLE_SERVICE_ACCOUNT_JSON not set")
	}
	if _, err := os.Stat(saJSON); err == nil {
		b, err := os.ReadFile(saJSON)
		if err != nil {
			return fmt.Errorf("failed to read service account file: %v", err)
		}
		saJSON = string(b)
	}
	spreadsheetID := os.Getenv("SPREADSHEET_ID")
	if spreadsheetID == "" {
		return fmt.Errorf("SPREADSHEET_ID not set")
	}

	ctx := context.Background()
	srv, err := sheets.NewService(ctx, option.WithCredentialsJSON([]byte(saJSON)))
	if err != nil {
		return fmt.Errorf("failed to create sheets service: %v", err)
	}
	sheetName := "users"
	sheetId, err := ensureSheetExists(ctx, srv, spreadsheetID, sheetName)
	if err != nil {
		return fmt.Errorf("ensure sheet failed: %v", err)
	}

	// Fetch DB rows and locate the specific user row
	dbRows, err := queryTableRows(database, "users")
	if err != nil || len(dbRows) == 0 {
		return fmt.Errorf("failed to query users table: %v", err)
	}
	dbHeader := dbRows[0]
	dbIndexLower := map[string]int{}
	for i, c := range dbHeader {
		dbIndexLower[strings.ToLower(fmt.Sprintf("%v", c))] = i
	}
	emailIdx, ok := dbIndexLower["email"]
	if !ok {
		return fmt.Errorf("users table missing email column")
	}
	var matched []interface{}
	for _, row := range dbRows[1:] {
		if emailIdx >= 0 && emailIdx < len(row) && strings.EqualFold(fmt.Sprintf("%v", row[emailIdx]), email) {
			matched = row
			break
		}
	}
	if matched == nil {
		return fmt.Errorf("user not found in DB: %s", email)
	}

	// Build desired headers (DB columns + flattened registrations)
	evs, _ := getEventDefs()
	desiredHeaders := buildUsersHeaders(dbHeader, evs)

	// Ensure header row is correct
	hdr, sheetRows, err := readSheetRows(ctx, srv, spreadsheetID, sheetName)
	if err != nil || len(hdr) == 0 {
		// Initialize header only
		hdrRow := make([]interface{}, len(desiredHeaders))
		for i, h := range desiredHeaders {
			hdrRow[i] = h
		}
		if _, err2 := srv.Spreadsheets.Values.Update(spreadsheetID, sheetName+"!A1", &sheets.ValueRange{Values: [][]interface{}{hdrRow}}).ValueInputOption("RAW").Do(); err2 != nil {
			return fmt.Errorf("failed to initialize users header: %v", err2)
		}
		hdr = desiredHeaders
		sheetRows = [][]string{}
	} else {
		needRewrite := len(hdr) != len(desiredHeaders)
		if !needRewrite {
			for i := range hdr {
				if i >= len(desiredHeaders) || !strings.EqualFold(hdr[i], desiredHeaders[i]) {
					needRewrite = true
					break
				}
			}
		}
		if needRewrite {
			hdrRow := make([]interface{}, len(desiredHeaders))
			for i, h := range desiredHeaders {
				hdrRow[i] = h
			}
			if _, err := srv.Spreadsheets.Values.Update(spreadsheetID, sheetName+"!A1", &sheets.ValueRange{Values: [][]interface{}{hdrRow}}).ValueInputOption("RAW").Do(); err != nil {
				return fmt.Errorf("failed to rewrite users header: %v", err)
			}
			hdr = desiredHeaders
		} else {
			hdr = desiredHeaders
		}
	}

	// Build the full row from DB according to headers
	dbIndexLower = map[string]int{}
	for i, c := range dbHeader {
		dbIndexLower[strings.ToLower(fmt.Sprintf("%v", c))] = i
	}
	rowVals := make([]interface{}, len(hdr))
	for ci, h := range hdr {
		// prefer users flattened mapping
		if val := getUsersValueForHeader(dbIndexLower, matched, h); val != "" {
			rowVals[ci] = val
			continue
		}
		if di, ok := dbIndexLower[strings.ToLower(h)]; ok && di >= 0 && di < len(matched) {
			rowVals[ci] = fmt.Sprintf("%v", matched[di])
		} else {
			rowVals[ci] = ""
		}
	}

	// Find row in sheet by email
	pkIdx := -1
	for i, h := range hdr {
		if strings.EqualFold(h, "email") {
			pkIdx = i
			break
		}
	}
	sr := -1
	if pkIdx >= 0 {
		for i, r := range sheetRows {
			if pkIdx < len(r) && strings.EqualFold(strings.TrimSpace(r[pkIdx]), email) {
				sr = i
				break
			}
		}
	}
	if sr == -1 {
		// append
		vr := &sheets.ValueRange{Values: [][]interface{}{rowVals}}
		_, err := srv.Spreadsheets.Values.Append(spreadsheetID, sheetName+"!A1", vr).ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("append failed: %v", err)
		}
		return nil
	}
	// overwrite existing row in a single update range
	lastCol := a1ColumnName(len(hdr) - 1)
	rowNum := sr + 2
	rng := fmt.Sprintf("%s!A%d:%s%d", sheetName, rowNum, lastCol, rowNum)
	vr := &sheets.ValueRange{Range: rng, Values: [][]interface{}{rowVals}}
	if _, err := srv.Spreadsheets.Values.Update(spreadsheetID, rng, vr).ValueInputOption("RAW").Context(ctx).Do(); err != nil {
		return fmt.Errorf("row update failed: %v", err)
	}
	// Style: freeze header in case the sheet was new
	_, _ = srv.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{Requests: []*sheets.Request{{UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{Properties: &sheets.SheetProperties{SheetId: sheetId, GridProperties: &sheets.GridProperties{FrozenRowCount: 1}}, Fields: "gridProperties.frozenRowCount"}}}}).Context(ctx).Do()
	return nil
}

func updateOnlyMissingCells(ctx context.Context, srv *sheets.Service, spreadsheetID, sheetName string, sheetId int64, pk string, headers []string, sheetRows [][]string, dbRows [][]interface{}) error {
	headerCount := len(headers)
	if headerCount == 0 {
		return fmt.Errorf("no headers")
	}

	// Build DB column name -> index map (case-insensitive)
	if len(dbRows) == 0 {
		return nil
	}
	dbHeader := dbRows[0]
	dbIndexLower := map[string]int{}
	for i, c := range dbHeader {
		dbIndexLower[strings.ToLower(fmt.Sprintf("%v", c))] = i
	}

	// Determine PK name and indices in sheet (for reading) and DB (for values)
	if pk == "" {
		pk = headers[0]
	}
	pkSheetIdx := -1
	for i, h := range headers {
		if strings.EqualFold(h, pk) {
			pkSheetIdx = i
			break
		}
	}
	if pkSheetIdx == -1 {
		pkSheetIdx = 0
	}
	dbPkIdx := -1
	if di, ok := dbIndexLower[strings.ToLower(pk)]; ok {
		dbPkIdx = di
	}

	// Map sheet PK value -> sheet row index
	sheetMap := map[string]int{}
	for i, r := range sheetRows {
		if pkSheetIdx < len(r) {
			v := strings.TrimSpace(r[pkSheetIdx])
			if v == "''" {
				v = ""
			}
			sheetMap[v] = i
		}
	}

	valueRanges := []*sheets.ValueRange{}
	appendRows := [][]interface{}{}

	// For each DB row, fill missing sheet cells by matching header names to DB columns
	for _, dbRow := range dbRows[1:] {
		var pkVal string
		if dbPkIdx >= 0 && dbPkIdx < len(dbRow) {
			pkVal = fmt.Sprintf("%v", dbRow[dbPkIdx])
		}
		if strings.TrimSpace(pkVal) == "" {
			continue
		}
		if sr, exists := sheetMap[pkVal]; exists {
			for ci := 0; ci < headerCount; ci++ {
				var sheetVal string
				if ci < len(sheetRows[sr]) {
					sheetVal = sheetRows[sr][ci]
				}
				// Find the DB column index for this header
				h := headers[ci]
				di, ok := dbIndexLower[strings.ToLower(h)]
				dbVal := ""
				if ok && di >= 0 && di < len(dbRow) {
					dbVal = fmt.Sprintf("%v", dbRow[di])
				}
				if sheetVal == "" {
					col := a1ColumnName(ci)
					rowNum := sr + 2
					rng := fmt.Sprintf("%s!%s%d", sheetName, col, rowNum)
					vr := &sheets.ValueRange{Range: rng, Values: [][]interface{}{{dbVal}}}
					valueRanges = append(valueRanges, vr)
				}
			}
		} else {
			// Append a new row, mapping by header name to DB values
			newRow := make([]interface{}, headerCount)
			for ci := 0; ci < headerCount; ci++ {
				h := headers[ci]
				if di, ok := dbIndexLower[strings.ToLower(h)]; ok && di >= 0 && di < len(dbRow) {
					dv := fmt.Sprintf("%v", dbRow[di])
					newRow[ci] = dv
				} else {
					// Try users flattened registration columns
					if strings.EqualFold(pk, "email") {
						val := getUsersValueForHeader(dbIndexLower, dbRow, h)
						newRow[ci] = val
					} else {
						newRow[ci] = ""
					}
				}
			}
			appendRows = append(appendRows, newRow)
		}
	}

	if len(valueRanges) > 0 {
		// Use a single BatchUpdate to reduce write requests
		_, _ = srv.Spreadsheets.Values.BatchUpdate(spreadsheetID, &sheets.BatchUpdateValuesRequest{
			ValueInputOption: "RAW",
			Data:             valueRanges,
		}).Context(ctx).Do()
	}
	if len(appendRows) > 0 {
		appendVR := &sheets.ValueRange{Values: appendRows}
		_, _ = srv.Spreadsheets.Values.Append(spreadsheetID, sheetName+"!A1", appendVR).ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	}

	// Do NOT delete extra sheet rows; allow admins to add users in the sheet which will be upserted into DB.
	return nil
}

// --- Sheets webhook (Apps Script) ---> DB updater ---

type sheetsRowUpdatePayload struct {
	Sheet   string   `json:"sheet"`
	PK      string   `json:"pk"`
	PKValue string   `json:"pk_value"`
	Headers []string `json:"headers"`
	Row     []string `json:"row"`
}

// SheetsRowUpdate receives a single-row update from Google Apps Script and applies it to the DB.
// Auth: header Authorization: Bearer <SHEETS_WEBHOOK_SECRET>
func SheetsRowUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Optional auth: if SHEETS_WEBHOOK_SECRET is set, require matching Bearer token.
	// If not set, allow public access (use with caution).
	if secret := strings.TrimSpace(os.Getenv("SHEETS_WEBHOOK_SECRET")); secret != "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(strings.ToLower(auth), strings.ToLower(prefix)) || strings.TrimSpace(auth[len(prefix):]) != secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	var payload sheetsRowUpdatePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(payload.Sheet) == "" || len(payload.Headers) == 0 || len(payload.Row) == 0 {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if globalDB == nil {
		http.Error(w, "db not ready", http.StatusServiceUnavailable)
		return
	}
	// Only allow users sheet for now
	sheet := strings.ToLower(strings.TrimSpace(payload.Sheet))
	if sheet != "users" {
		http.Error(w, "unsupported sheet", http.StatusBadRequest)
		return
	}
	pk := payload.PK
	if pk == "" {
		pk = "email"
	}
	// Ensure row is same length as headers
	row := payload.Row
	if len(row) < len(payload.Headers) {
		padded := make([]string, len(payload.Headers))
		copy(padded, row)
		row = padded
	} else if len(row) > len(payload.Headers) {
		row = row[:len(payload.Headers)]
	}
	// Apply in a critical section shared with the periodic sync
	sheetsOpMu.Lock()
	defer sheetsOpMu.Unlock()
	if err := applyTableUpserts(globalDB, sheet, pk, payload.Headers, [][]string{row}, nil); err != nil {
		log.Printf("sheets webhook upsert error: %v", err)
		http.Error(w, "upsert failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// SheetsHeaders returns the expected header row for a given sheet (currently only users),
// so the Apps Script can initialize the sheet correctly.
// Auth: Authorization: Bearer <SHEETS_WEBHOOK_SECRET>
func SheetsHeaders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Optional auth
	if secret := strings.TrimSpace(os.Getenv("SHEETS_WEBHOOK_SECRET")); secret != "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(strings.ToLower(auth), strings.ToLower(prefix)) || strings.TrimSpace(auth[len(prefix):]) != secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	if globalDB == nil {
		http.Error(w, "db not ready", http.StatusServiceUnavailable)
		return
	}
	sheet := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sheet")))
	if sheet == "" {
		sheet = "users"
	}
	if sheet != "users" {
		http.Error(w, "unsupported sheet", http.StatusBadRequest)
		return
	}
	// Build users headers from DB columns + events
	dbRows, err := queryTableRows(globalDB, "users")
	if err != nil || len(dbRows) == 0 {
		http.Error(w, "failed to inspect users table", http.StatusInternalServerError)
		return
	}
	evs, _ := getEventDefs()
	headers := buildUsersHeaders(dbRows[0], evs)
	resp := map[string]interface{}{
		"sheet":   sheet,
		"pk":      "email",
		"headers": headers,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// SheetsPing: simple auth-checked endpoint for Apps Script setup testing
func SheetsPing(w http.ResponseWriter, r *http.Request) {
	// Optional auth
	if secret := strings.TrimSpace(os.Getenv("SHEETS_WEBHOOK_SECRET")); secret != "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(strings.ToLower(auth), strings.ToLower(prefix)) || strings.TrimSpace(auth[len(prefix):]) != secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
