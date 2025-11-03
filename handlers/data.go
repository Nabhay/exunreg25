package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
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

	eventsCap := map[string]int{}
	evRows, err := queryTableRows(database, "events")
	if err == nil {
		for _, er := range evRows[1:] {
			id := fmt.Sprintf("%v", er[0])
			capStr := fmt.Sprintf("%v", er[6])
			if capStr == "" {
				continue
			}
			if v, err := strconv.Atoi(capStr); err == nil {
				eventsCap[id] = v
			}
		}
	}

	usersRows, err := queryTableRows(database, "users")
	if err == nil {
		if len(usersRows) > 0 {
			userHeader := usersRows[0]
			userIndexLower := map[string]int{}
			for i, c := range userHeader {
				key := strings.ToLower(fmt.Sprintf("%v", c))
				userIndexLower[key] = i
			}
			unameIdx := -1
			regsIdx := -1
			instIdx := -1
			updatedIdx := -1
			if v, ok := userIndexLower["username"]; ok {
				unameIdx = v
			}
			if v, ok := userIndexLower["registrations"]; ok {
				regsIdx = v
			}
			if v, ok := userIndexLower["institution_name"]; ok {
				instIdx = v
			} else if v, ok := userIndexLower["institution"]; ok {
				instIdx = v
			}
			if v, ok := userIndexLower["updated_at"]; ok {
				updatedIdx = v
			}

			upsertQ := `INSERT INTO usr_regs (username, institution, event_id, p1_name, p1_email, p1_class, p1_phone, p2_name, p2_email, p2_class, p2_phone, p3_name, p3_email, p3_class, p3_phone, p4_name, p4_email, p4_class, p4_phone, p5_name, p5_email, p5_class, p5_phone, p6_name, p6_email, p6_class, p6_phone, p7_name, p7_email, p7_class, p7_phone, p8_name, p8_email, p8_class, p8_phone, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(username, event_id) DO UPDATE SET p1_name = excluded.p1_name, p1_email = excluded.p1_email, p1_class = excluded.p1_class, p1_phone = excluded.p1_phone, p2_name = excluded.p2_name, p2_email = excluded.p2_email, p2_class = excluded.p2_class, p2_phone = excluded.p2_phone, p3_name = excluded.p3_name, p3_email = excluded.p3_email, p3_class = excluded.p3_class, p3_phone = excluded.p3_phone, p4_name = excluded.p4_name, p4_email = excluded.p4_email, p4_class = excluded.p4_class, p4_phone = excluded.p4_phone, p5_name = excluded.p5_name, p5_email = excluded.p5_email, p5_class = excluded.p5_class, p5_phone = excluded.p5_phone, p6_name = excluded.p6_name, p6_email = excluded.p6_email, p6_class = excluded.p6_class, p6_phone = excluded.p6_phone, p7_name = excluded.p7_name, p7_email = excluded.p7_email, p7_class = excluded.p7_class, p7_phone = excluded.p7_phone, p8_name = excluded.p8_name, p8_email = excluded.p8_email, p8_class = excluded.p8_class, p8_phone = excluded.p8_phone, updated_at = excluded.updated_at`

			const maxParts = 8
			for _, ur := range usersRows[1:] {
				username := ""
				if unameIdx >= 0 && unameIdx < len(ur) {
					username = fmt.Sprintf("%v", ur[unameIdx])
				}
				institution := ""
				if instIdx >= 0 && instIdx < len(ur) {
					institution = fmt.Sprintf("%v", ur[instIdx])
				}
				regsRaw := ""
				if regsIdx >= 0 && regsIdx < len(ur) {
					regsRaw = fmt.Sprintf("%v", ur[regsIdx])
				}
				if regsRaw == "" || regsRaw == "{}" {
					continue
				}
				var regs map[string][]db.Participant
				if err := json.Unmarshal([]byte(regsRaw), &regs); err != nil {
					continue
				}
				userUpdated := time.Now()
				if updatedIdx >= 0 && updatedIdx < len(ur) {
					s := fmt.Sprintf("%v", ur[updatedIdx])
					if t, err := parseFlexibleTime(s); err == nil && !t.IsZero() {
						userUpdated = t
					}
				}
				now := time.Now()
				for evID, parts := range regs {
					var existingUpdated sql.NullString
					err := database.QueryRow("SELECT updated_at FROM usr_regs WHERE username = ? AND event_id = ?", username, evID).Scan(&existingUpdated)
					if err != nil && err != sql.ErrNoRows {
						continue
					}
					if err == nil {
						existingT, _ := parseFlexibleTime(fmt.Sprintf("%v", existingUpdated.String))
						if !userUpdated.After(existingT) {
							continue
						}
					}
					args := []interface{}{username, institution, evID}
					for i := 0; i < maxParts; i++ {
						if i < len(parts) {
							args = append(args, parts[i].Name)
							args = append(args, parts[i].Email)
							args = append(args, fmt.Sprintf("%v", parts[i].Class))
							args = append(args, parts[i].Phone)
						} else {
							args = append(args, "", "", "", "")
						}
					}
					args = append(args, now, userUpdated)
					_, _ = database.Exec(upsertQ, args...)
				}
			}
		}
	}

	primaryKeys := map[string]string{
		"users":                    "email",
		"events":                   "id",
		"individual_registrations": "id",
		"usr_regs":                 "id",
	}

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
			vr := &sheets.ValueRange{Values: convertToValues(rows)}
			rangeA1 := sheetName + "!A1"
			_, err = srv.Spreadsheets.Values.Update(spreadsheetID, rangeA1, vr).ValueInputOption("RAW").Do()
			if err != nil {
				log.Printf("failed to update sheet %s: %v", sheetName, err)
			}
			continue
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
						t, _ := parseFlexibleTime(s)
						sheetUpdated = t
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
						t, _ := parseFlexibleTime(rawDB)
						dbUpdated = t
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
			if len(sheetPKs) > 0 {
				if err := deleteDBRowsNotInSheet(database, t, pk, sheetPKs); err != nil {
					log.Printf("failed to delete missing rows for table %s: %v", t, err)
				}
			} else {
				log.Printf("sheet %s has no primary-key values; skipping delete to avoid wiping DB", sheetName)
			}
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
						for ci := 0; ci < colsCount; ci++ {
							h := hdr[ci]
							if di, ok := dbIndex[h]; ok && di >= 0 && di < len(dbRow) {
								newRow[ci] = fmt.Sprintf("%v", dbRow[di])
							} else {
								newRow[ci] = ""
							}
						}
						vr := &sheets.ValueRange{Values: [][]interface{}{newRow}}
						_, _ = srv.Spreadsheets.Values.Append(spreadsheetID, sheetName+"!A1", vr).ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
						continue
					}
					log.Printf("db->sheet: table=%s pk=%s dbUpdated=%v sheetUpdated=%v exists=%v skipped=%v", t, pkVal, dbUpdated, sheetUpdated, exists, skippedSet[pkVal])
					if dbUpdated.After(sheetUpdated) || skippedSet[pkVal] {
						rowNum := sr + 2
						for ci := 0; ci < colsCount; ci++ {
							h := hdr[ci]
							var dbVal string
							if di, ok := dbIndex[h]; ok && di >= 0 && di < len(dbRow) {
								dbVal = fmt.Sprintf("%v", dbRow[di])
							} else {
								dbVal = ""
							}
							var sheetVal string
							if ci < len(sheetRows[sr]) {
								sheetVal = sheetRows[sr][ci]
							} else {
								sheetVal = ""
							}
							if strings.TrimSpace(dbVal) != strings.TrimSpace(sheetVal) {
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

	if err := mergeUsrRegsToUsers(database); err != nil {
		log.Printf("merge usr_regs->users error: %v", err)
	}
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
	rng := sheetName + "!A1:Z"
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
	dbPkIdx := -1
	if len(dbRowsAll) > 0 {
		for i, c := range dbRowsAll[0] {
			dbIndex[fmt.Sprintf("%v", c)] = i
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
	defer func() {
		if err != nil {
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()

	for _, r := range rows {
		values := map[string]string{}
		for i, h := range headers {
			v := r[i]
			if strings.TrimSpace(v) == "''" {
				v = ""
			}
			values[h] = v
		}
		keyVal := values[pk]
		if keyVal == "" {
			continue
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
			cols = append(cols, h)
			placeholders = append(placeholders, "?")
			v := values[h]
			if strings.EqualFold(h, "updated_at") {
				args = append(args, time.Now())
				continue
			}
			vtrim := strings.TrimSpace(v)
			if vtrim == "" {
				args = append(args, nil)
			} else {
				args = append(args, v)
			}
			if h != pk {
				updates = append(updates, fmt.Sprintf("%s = COALESCE(excluded.%s, %s)", h, h, h))
			}
		}
		q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s", table, strings.Join(cols, ", "), strings.Join(placeholders, ", "), pk, strings.Join(updates, ", "))
		if _, err := tx.Exec(q, args...); err != nil {
			log.Printf("upsert error table %s key %s: %v", table, keyVal, err)
			continue
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

func updateOnlyMissingCells(ctx context.Context, srv *sheets.Service, spreadsheetID, sheetName string, sheetId int64, pk string, headers []string, sheetRows [][]string, dbRows [][]interface{}) error {
	headerCount := len(headers)
	sheetMap := map[string]int{}
	pkIndex := -1
	if headerCount == 0 {
		return fmt.Errorf("no headers")
	}
	if pk == "" {
		pk = headers[0]
	}
	for i, h := range headers {
		if strings.EqualFold(h, pk) {
			pkIndex = i
			break
		}
	}
	if pkIndex == -1 {
		pkIndex = 0
	}
	for i, r := range sheetRows {
		if pkIndex < len(r) {
			v := strings.TrimSpace(r[pkIndex])
			if v == "''" {
				v = ""
			}
			sheetMap[v] = i
		}
	}

	valueRanges := []*sheets.ValueRange{}
	appendRows := [][]interface{}{}

	for ri, dbRow := range dbRows[1:] {
		pkVal := fmt.Sprintf("%v", dbRow[pkIndex])
		if pkVal == "" {
			continue
		}
		if sr, exists := sheetMap[pkVal]; exists {
			for ci := 0; ci < headerCount; ci++ {
				var sheetVal string
				if ci < len(sheetRows[sr]) {
					sheetVal = sheetRows[sr][ci]
				}
				dbVal := ""
				if ci < len(dbRow) {
					dbVal = fmt.Sprintf("%v", dbRow[ci])
				}
				writeVal := dbVal
				if dbVal == "" {
					writeVal = ""
				}
				if sheetVal == "" {
					col := a1ColumnName(ci)
					rowNum := sr + 2
					rng := fmt.Sprintf("%s!%s%d", sheetName, col, rowNum)
					vr := &sheets.ValueRange{Range: rng, Values: [][]interface{}{{writeVal}}}
					valueRanges = append(valueRanges, vr)
				}
			}
		} else {
			newRow := make([]interface{}, headerCount)
			for ci := 0; ci < headerCount; ci++ {
				if ci < len(dbRow) {
					dv := fmt.Sprintf("%v", dbRow[ci])
					if dv == "" {
						newRow[ci] = ""
					} else {
						newRow[ci] = dv
					}
				} else {
					newRow[ci] = ""
				}
			}
			appendRows = append(appendRows, newRow)
		}
		_ = ri
	}

	if len(valueRanges) > 0 {
		for _, vr := range valueRanges {
			_, _ = srv.Spreadsheets.Values.Update(spreadsheetID, vr.Range, vr).ValueInputOption("RAW").Context(ctx).Do()
		}
	}
	if len(appendRows) > 0 {
		appendVR := &sheets.ValueRange{Values: appendRows}
		_, _ = srv.Spreadsheets.Values.Append(spreadsheetID, sheetName+"!A1", appendVR).ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	}

	dbCount := len(dbRows) - 1
	sheetCount := len(sheetRows)
	if sheetCount > dbCount {
		start := dbCount + 1
		end := sheetCount
		deleteReq := &sheets.DeleteDimensionRequest{
			Range: &sheets.DimensionRange{
				SheetId:    sheetId,
				Dimension:  "ROWS",
				StartIndex: int64(start + 1),
				EndIndex:   int64(end + 1),
			},
		}
		batch := &sheets.BatchUpdateSpreadsheetRequest{Requests: []*sheets.Request{{DeleteDimension: deleteReq}}}
		_, _ = srv.Spreadsheets.BatchUpdate(spreadsheetID, batch).Context(ctx).Do()
	}
	return nil
}
