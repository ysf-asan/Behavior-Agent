package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return store, nil
}

func (s *SQLiteStore) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			start_time TEXT NOT NULL,
			end_time TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			risk_score TEXT,
			features TEXT,
			event_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS behavior_events (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			user_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			data TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session ON behavior_events(session_id, timestamp)`,
		`CREATE TABLE IF NOT EXISTS risk_scores (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			overall REAL NOT NULL,
			keystroke REAL NOT NULL DEFAULT 0,
			mouse REAL NOT NULL DEFAULT 0,
			scroll REAL NOT NULL DEFAULT 0,
			click REAL NOT NULL DEFAULT 0,
			level TEXT NOT NULL DEFAULT 'low'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_risk_session ON risk_scores(session_id, timestamp DESC)`,
		`CREATE TABLE IF NOT EXISTS user_profiles (
			user_id TEXT PRIMARY KEY,
			sessions TEXT NOT NULL DEFAULT '[]',
			avg_risk_score REAL NOT NULL DEFAULT 0,
			last_active TEXT NOT NULL,
			model_trained INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS profiles (
			user_id TEXT PRIMARY KEY,
			profile_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) CreateSessionTable(ctx context.Context) error {
	return nil
}

func (s *SQLiteStore) SaveSession(ctx context.Context, session interface{}) error {
	m, ok := session.(map[string]interface{})
	if !ok {
		return fmt.Errorf("session must be map[string]interface{}")
	}
	id, _ := m["id"].(string)
	userID, _ := m["userId"].(string)
	startTime, _ := m["startTime"].(string)
	status, _ := m["status"].(string)

	endTime := ""
	if v, ok := m["endTime"].(string); ok {
		endTime = v
	}
	riskScore := "null"
	if v, ok := m["riskScore"]; ok {
		b, _ := json.Marshal(v)
		riskScore = string(b)
	}
	features := "null"
	if v, ok := m["features"]; ok {
		b, _ := json.Marshal(v)
		features = string(b)
	}
	eventCount := 0
	if v, ok := m["eventCount"].(float64); ok {
		eventCount = int(v)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO sessions (id, user_id, start_time, end_time, status, risk_score, features, event_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, startTime, endTime, status, riskScore, features, eventCount)
	return err
}

func (s *SQLiteStore) GetSession(ctx context.Context, sessionID string) (interface{}, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, start_time, end_time, status, risk_score, features, event_count
		 FROM sessions WHERE id = ?`, sessionID)

	var id, userID, startTime, status string
	var endTime, riskJSON, featuresJSON sql.NullString
	var eventCount int

	if err := row.Scan(&id, &userID, &startTime, &endTime, &status, &riskJSON, &featuresJSON, &eventCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, err
	}

	session := map[string]interface{}{
		"id":         id,
		"userId":     userID,
		"startTime":  startTime,
		"status":     status,
		"eventCount": eventCount,
	}
	if endTime.Valid {
		session["endTime"] = endTime.String
	}
	if riskJSON.Valid && riskJSON.String != "null" {
		var rs interface{}
		json.Unmarshal([]byte(riskJSON.String), &rs)
		session["riskScore"] = rs
	}
	if featuresJSON.Valid && featuresJSON.String != "null" {
		var f interface{}
		json.Unmarshal([]byte(featuresJSON.String), &f)
		session["features"] = f
	}
	return session, nil
}

func (s *SQLiteStore) UpdateSession(ctx context.Context, session interface{}) error {
	return s.SaveSession(ctx, session)
}

func (s *SQLiteStore) SaveEvent(ctx context.Context, event interface{}) error {
	m, ok := event.(map[string]interface{})
	if !ok {
		return fmt.Errorf("event must be map[string]interface{}")
	}
	eType, _ := m["type"].(string)
	userID, _ := m["userId"].(string)
	sessionID, _ := m["sessionId"].(string)
	data, _ := json.Marshal(m["data"])
	timestamp := time.Now().Format(time.RFC3339Nano)
	if v, ok := m["timestamp"].(string); ok && v != "" {
		timestamp = v
	}

	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO behavior_events (id, type, user_id, session_id, timestamp, data)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, eType, userID, sessionID, timestamp, string(data))
	return err
}

func (s *SQLiteStore) GetEvents(ctx context.Context, sessionID string, limit int32, nextToken *string) ([]interface{}, *string, error) {
	offset := 0
	if nextToken != nil {
		fmt.Sscanf(*nextToken, "%d", &offset)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, user_id, session_id, timestamp, data
		 FROM behavior_events WHERE session_id = ?
		 ORDER BY timestamp LIMIT ? OFFSET ?`,
		sessionID, limit, offset)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var events []interface{}
	for rows.Next() {
		var id, etype, userID, sessionID, timestamp, data string
		if err := rows.Scan(&id, &etype, &userID, &sessionID, &timestamp, &data); err != nil {
			return nil, nil, err
		}
		event := map[string]interface{}{
			"type":      etype,
			"userId":    userID,
			"sessionId": sessionID,
			"timestamp": timestamp,
		}
		var d interface{}
		json.Unmarshal([]byte(data), &d)
		event["data"] = d
		events = append(events, event)
	}

	nextOffset := offset + len(events)
	token := fmt.Sprintf("%d", nextOffset)
	return events, &token, nil
}

func (s *SQLiteStore) SaveRiskScore(ctx context.Context, sessionID string, score interface{}) error {
	m, ok := score.(map[string]interface{})
	if !ok {
		return fmt.Errorf("score must be map[string]interface{}")
	}
	timestamp := time.Now().Format(time.RFC3339Nano)
	if v, ok := m["timestamp"].(string); ok && v != "" {
		timestamp = v
	}
	overall, _ := m["overall"].(float64)
	keystroke, _ := m["keystroke"].(float64)
	mouse, _ := m["mouse"].(float64)
	scroll, _ := m["scroll"].(float64)
	click, _ := m["click"].(float64)
	level, _ := m["level"].(string)
	if level == "" {
		level = "low"
	}

	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO risk_scores (id, session_id, timestamp, overall, keystroke, mouse, scroll, click, level)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sessionID, timestamp, overall, keystroke, mouse, scroll, click, level)
	return err
}

func (s *SQLiteStore) GetLatestRiskScore(ctx context.Context, sessionID string) (interface{}, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT timestamp, overall, keystroke, mouse, scroll, click, level
		 FROM risk_scores WHERE session_id = ?
		 ORDER BY timestamp DESC LIMIT 1`, sessionID)

	var timestamp string
	var overall, keystroke, mouse, scroll, click float64
	var level string
	if err := row.Scan(&timestamp, &overall, &keystroke, &mouse, &scroll, &click, &level); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no risk score found for session: %s", sessionID)
		}
		return nil, err
	}
	return map[string]interface{}{
		"timestamp": timestamp,
		"overall":   overall,
		"keystroke": keystroke,
		"mouse":     mouse,
		"scroll":    scroll,
		"click":     click,
		"level":     level,
	}, nil
}

func (s *SQLiteStore) GetRiskHistory(ctx context.Context, sessionID string, limit int32) ([]interface{}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT timestamp, overall, keystroke, mouse, scroll, click, level
		 FROM risk_scores WHERE session_id = ?
		 ORDER BY timestamp DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scores []interface{}
	for rows.Next() {
		var timestamp string
		var overall, keystroke, mouse, scroll, click float64
		var level string
		if err := rows.Scan(&timestamp, &overall, &keystroke, &mouse, &scroll, &click, &level); err != nil {
			return nil, err
		}
		scores = append(scores, map[string]interface{}{
			"timestamp": timestamp,
			"overall":   overall,
			"keystroke": keystroke,
			"mouse":     mouse,
			"scroll":    scroll,
			"click":     click,
			"level":     level,
		})
	}
	return scores, nil
}

func (s *SQLiteStore) SaveUserProfile(ctx context.Context, profile interface{}) error {
	m, ok := profile.(map[string]interface{})
	if !ok {
		return fmt.Errorf("profile must be map[string]interface{}")
	}
	userID, _ := m["userId"].(string)
	sessions, _ := json.Marshal(m["sessions"])
	avgRiskScore, _ := m["avgRiskScore"].(float64)
	lastActive, _ := m["lastActive"].(string)
	if lastActive == "" {
		lastActive = time.Now().Format(time.RFC3339Nano)
	}
	modelTrained := 0
	if v, ok := m["modelTrained"].(bool); ok && v {
		modelTrained = 1
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_profiles (user_id, sessions, avg_risk_score, last_active, model_trained)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, string(sessions), avgRiskScore, lastActive, modelTrained)
	return err
}

func (s *SQLiteStore) GetUserProfile(ctx context.Context, userID string) (interface{}, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT user_id, sessions, avg_risk_score, last_active, model_trained
		 FROM user_profiles WHERE user_id = ?`, userID)

	var uid, sessionsJSON, lastActive string
	var avgRiskScore float64
	var modelTrained int

	if err := row.Scan(&uid, &sessionsJSON, &avgRiskScore, &lastActive, &modelTrained); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user profile not found: %s", userID)
		}
		return nil, err
	}

	var sessions []string
	json.Unmarshal([]byte(sessionsJSON), &sessions)

	return map[string]interface{}{
		"userId":       uid,
		"sessions":     sessions,
		"avgRiskScore": avgRiskScore,
		"lastActive":   lastActive,
		"modelTrained": modelTrained != 0,
	}, nil
}

func (s *SQLiteStore) ListUsers(ctx context.Context) ([]interface{}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, sessions, avg_risk_score, last_active, model_trained FROM user_profiles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []interface{}
	for rows.Next() {
		var uid, sessionsJSON, lastActive string
		var avgRiskScore float64
		var modelTrained int

		if err := rows.Scan(&uid, &sessionsJSON, &avgRiskScore, &lastActive, &modelTrained); err != nil {
			return nil, err
		}
		var sessions []string
		json.Unmarshal([]byte(sessionsJSON), &sessions)
		profiles = append(profiles, map[string]interface{}{
			"userId":       uid,
			"sessions":     sessions,
			"avgRiskScore": avgRiskScore,
			"lastActive":   lastActive,
			"modelTrained": modelTrained != 0,
		})
	}
	return profiles, nil
}

func (s *SQLiteStore) SaveProfileBlob(ctx context.Context, userID string, profileJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO profiles (user_id, profile_json, updated_at)
		 VALUES (?, ?, ?)`,
		userID, profileJSON, time.Now().Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) GetProfileBlob(ctx context.Context, userID string) (string, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT profile_json FROM profiles WHERE user_id = ?`, userID)

	var profileJSON string
	if err := row.Scan(&profileJSON); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("profile not found: %s", userID)
		}
		return "", err
	}
	return profileJSON, nil
}

func (s *SQLiteStore) SaveSettings(ctx context.Context, key string, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)`,
		key, value)
	return err
}

func (s *SQLiteStore) GetSettings(ctx context.Context, key string) (string, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = ?`, key)

	var value string
	if err := row.Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("setting not found: %s", key)
		}
		return "", err
	}
	return value, nil
}
