package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ysf-asan/behavior-agent/internal/features"
	"github.com/ysf-asan/behavior-agent/internal/ml"
	"github.com/ysf-asan/behavior-agent/internal/store"
)

type AgentAPI struct {
	router     *gin.Engine
	profileMgr *ml.ProfileManager
	extractor  *features.Extractor
	store      *store.SQLiteStore
	port       string
	capturer   interface {
		Start() error
		Stop() error
	}
	onTrain    func()
	onReset    func()
	featMu     sync.Mutex
	featHistory []features.FeatureVector
}

type Option func(*AgentAPI)

func WithCapturer(c interface{ Start() error; Stop() error }) Option {
	return func(a *AgentAPI) {
		a.capturer = c
	}
}

func WithOnTrain(fn func()) Option {
	return func(a *AgentAPI) {
		a.onTrain = fn
	}
}

func WithOnReset(fn func()) Option {
	return func(a *AgentAPI) {
		a.onReset = fn
	}
}

func New(port string, st *store.SQLiteStore, pm *ml.ProfileManager, ext *features.Extractor, opts ...Option) *AgentAPI {
	a := &AgentAPI{
		router:     gin.New(),
		profileMgr: pm,
		extractor:  ext,
		store:      st,
		port:       port,
	}
	for _, opt := range opts {
		opt(a)
	}
	a.router.Use(gin.Recovery())
	a.router.Use(corsMiddleware())
	a.setupRoutes()
	return a
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

type statusResponse struct {
	Running       bool   `json:"running"`
	Learning      bool   `json:"learning"`
	SampleCount   int    `json:"sampleCount"`
	ProfileStatus string `json:"profileStatus"`
	WindowTitle   string `json:"windowTitle"`
}

type profileResponse struct {
	UserID      string `json:"userId"`
	Status      string `json:"status"`
	SampleCount int    `json:"sampleCount"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type trainRequest struct {
	UserID string `json:"userId"`
}

type riskResponse struct {
	AnomalyScore float64 `json:"anomalyScore"`
	RiskLevel    string  `json:"riskLevel"`
	IsAnomaly    bool    `json:"isAnomaly"`
	Timestamp    string  `json:"timestamp"`
}

type startupRequest struct {
	Enabled bool `json:"enabled"`
}

type startupResponse struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}

type eventEntry struct {
	Timestamp string    `json:"timestamp"`
	Features  []float64 `json:"features"`
}

func (a *AgentAPI) setupRoutes() {
	a.router.GET("/api/status", a.handleStatus)
	a.router.GET("/api/profile", a.handleProfile)
	a.router.POST("/api/train", a.handleTrain)
	a.router.GET("/api/risk", a.handleRisk)
	a.router.GET("/api/events", a.handleEvents)
	a.router.POST("/api/startup", a.handleSetStartup)
	a.router.GET("/api/startup", a.handleGetStartup)
}

func (a *AgentAPI) handleStatus(c *gin.Context) {
	profile := a.profileMgr.GetProfile()
	profileStatus := ""
	if profile != nil {
		profileStatus = profile.Status
	}
	windowTitle := ""
	if a.capturer != nil {
		if w, ok := a.capturer.(interface{ GetActiveWindowTitle() string }); ok {
			windowTitle = w.GetActiveWindowTitle()
		}
	}

	c.JSON(http.StatusOK, statusResponse{
		Running:       a.capturer != nil,
		Learning:      a.profileMgr.GetProfile() == nil || a.profileMgr.GetProfile().Status != "ready",
		SampleCount:   a.profileMgr.SampleCount(),
		ProfileStatus: profileStatus,
		WindowTitle:   windowTitle,
	})
}

func (a *AgentAPI) handleProfile(c *gin.Context) {
	profile := a.profileMgr.GetProfile()
	if profile == nil {
		c.JSON(http.StatusOK, profileResponse{Status: "not_ready"})
		return
	}
	c.JSON(http.StatusOK, profileResponse{
		UserID:      profile.UserID,
		Status:      profile.Status,
		SampleCount: profile.SampleCount,
		CreatedAt:   profile.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:   profile.UpdatedAt.Format(time.RFC3339Nano),
	})
}

func (a *AgentAPI) handleTrain(c *gin.Context) {
	var req trainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if a.profileMgr.SampleCount() < 30 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient samples: need at least 30"})
		return
	}

	if err := a.profileMgr.Train(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	profile := a.profileMgr.GetProfile()
	if profile != nil {
		data, _ := json.Marshal(profile)
		if a.store != nil && req.UserID != "" {
			a.store.SaveProfileBlob(c.Request.Context(), req.UserID, string(data))
			sessionData := map[string]interface{}{
				"userId":    req.UserID,
				"status":    "trained",
				"startTime": time.Now().Format(time.RFC3339Nano),
			}
			a.store.SaveSession(c.Request.Context(), sessionData)
		}
	}

	if a.onTrain != nil {
		a.onTrain()
	}

	c.JSON(http.StatusOK, gin.H{"status": "trained", "samples": a.profileMgr.SampleCount()})
}

func (a *AgentAPI) handleRisk(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		sessionID = "default"
	}

	vec := a.extractor.Flush()
	if vec != nil {
		a.featMu.Lock()
		a.featHistory = append(a.featHistory, vec)
		if len(a.featHistory) > 100 {
			a.featHistory = a.featHistory[len(a.featHistory)-100:]
		}
		a.featMu.Unlock()

		normalized := a.profileMgr.Normalize(vec)
		result, err := a.profileMgr.Predict(normalized)
		if err == nil {
			if a.store != nil {
				scoreData := map[string]interface{}{
					"overall":   result.AnomalyScore,
					"keystroke": 0.0,
					"mouse":     0.0,
					"scroll":    0.0,
					"click":     0.0,
					"level":     result.RiskLevel,
					"timestamp": time.Now().Format(time.RFC3339Nano),
				}
				a.store.SaveRiskScore(c.Request.Context(), sessionID, scoreData)
			}

			c.JSON(http.StatusOK, riskResponse{
				AnomalyScore: result.AnomalyScore,
				RiskLevel:    result.RiskLevel,
				IsAnomaly:    result.IsAnomaly,
				Timestamp:    time.Now().Format(time.RFC3339Nano),
			})
			return
		}
	}

	c.JSON(http.StatusOK, riskResponse{
		AnomalyScore: 0,
		RiskLevel:    "unknown",
		IsAnomaly:    false,
		Timestamp:    time.Now().Format(time.RFC3339Nano),
	})
}

func (a *AgentAPI) handleEvents(c *gin.Context) {
	a.featMu.Lock()
	history := make([]eventEntry, len(a.featHistory))
	for i, fv := range a.featHistory {
		history[i] = eventEntry{
			Timestamp: time.Now().Format(time.RFC3339Nano),
			Features:  []float64(fv),
		}
	}
	a.featMu.Unlock()

	if history == nil {
		history = []eventEntry{}
	}
	c.JSON(http.StatusOK, history)
}

func (a *AgentAPI) handleSetStartup(c *gin.Context) {
	var req startupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if err := setStartupEnabled(req.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"enabled": req.Enabled})
}

func (a *AgentAPI) handleGetStartup(c *gin.Context) {
	startupDir := getStartupDir()
	batPath := filepath.Join(startupDir, "BehaviorDNA_Agent.bat")
	enabled := false
	if _, err := os.Stat(batPath); err == nil {
		enabled = true
	}
	c.JSON(http.StatusOK, startupResponse{
		Enabled: enabled,
		Path:    batPath,
	})
}

func (a *AgentAPI) Run() error {
	addr := ":" + a.port
	if addr == ":" {
		addr = ":9090"
	}
	return a.router.Run(addr)
}

func getStartupDir() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
}

var writeFile = os.WriteFile

func setStartupEnabled(enabled bool) error {
	startupDir := getStartupDir()
	if err := os.MkdirAll(startupDir, 0755); err != nil {
		return err
	}
	batPath := filepath.Join(startupDir, "BehaviorDNA_Agent.bat")
	if !enabled {
		if err := os.Remove(batPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	content := "@echo off\nstart \"\" \"" + exe + "\"\n"
	return writeFile(batPath, []byte(content), 0644)
}
