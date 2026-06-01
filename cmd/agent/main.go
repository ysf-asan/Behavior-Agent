package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ysf-asan/behavior-agent/internal/api"
	"github.com/ysf-asan/behavior-agent/internal/capturer"
	"github.com/ysf-asan/behavior-agent/internal/features"
	"github.com/ysf-asan/behavior-agent/internal/ml"
	"github.com/ysf-asan/behavior-agent/internal/store"
	"github.com/ysf-asan/behavior-agent/internal/tray"
)

const (
	version         = "1.0.0"
	defaultPort     = "9090"
	sampleInterval  = 15 * time.Second
	trainThreshold  = 30
)

type App struct {
	store      *store.SQLiteStore
	capturer   *capturer.Capturer
	extractor  *features.Extractor
	profileMgr *ml.ProfileManager
	tray       *tray.Tray
	api        *api.AgentAPI
	stopCh     chan struct{}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("BehaviorDNA Agent v" + version)

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		exe, _ := os.Executable()
		dbPath = filepath.Join(filepath.Dir(exe), "behaviordna.db")
	}
	port := os.Getenv("API_PORT")
	if port == "" {
		port = defaultPort
	}

	app := &App{stopCh: make(chan struct{})}

	var err error
	app.store, err = store.NewSQLiteStore(dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	log.Println("store initialised")

	app.extractor = features.NewExtractor()
	app.profileMgr = ml.NewProfileManager()

	app.capturer = capturer.New()

	app.tray = tray.New("BehaviorDNA", func() {
		log.Println("exiting from tray")
		closeSafe(app.stopCh)
	})

	app.tray.SetMenu([]tray.MenuItem{
		{ID: 1, Label: "Dashboard'i Ac", Clicked: func() {
			exec.Command("cmd", "/c", "start", "http://localhost:"+port).Start()
		}},
		{ID: 2, Label: "Profili Egit", Clicked: func() { doTrain(app) }},
		{ID: 4, Label: "Cikis", Clicked: func() { closeSafe(app.stopCh) }},
	})

	opts := []api.Option{
		api.WithCapturer(app.capturer),
		api.WithOnTrain(func() { doTrain(app) }),
		api.WithOnReset(func() { doReset(app) }),
	}
	app.api = api.New(port, app.store, app.profileMgr, app.extractor, opts...)

	loadProfile(app)

	go app.run()
	go func() {
		if err := app.api.Run(); err != nil {
			log.Printf("api server error: %v", err)
		}
	}()

	if err := app.tray.Show(); err != nil {
		log.Printf("tray show error: %v", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case <-app.stopCh:
	}

	shutdown(app)
}

func (app *App) run() {
	if err := app.capturer.Start(); err != nil {
		log.Printf("capturer start failed: %v", err)
		return
	}
	defer app.capturer.Stop()

	app.updateTrayIcon()
	processTicker := time.NewTicker(sampleInterval)
	defer processTicker.Stop()

	for {
		select {
		case <-app.stopCh:
			return
		case evt := <-app.capturer.Events:
			app.extractor.Add(features.RawEvent{
				Type:      evt.Type,
				Timestamp: evt.Timestamp,
				Data:      evt.Data,
			})
		case <-processTicker.C:
			app.processWindow()
		}
	}
}

func (app *App) processWindow() {
	vec := app.extractor.Flush()
	pm := app.profileMgr

	if pm.GetProfile() == nil || pm.GetProfile().Status != "ready" {
		if vec != nil {
			pm.AddSample(vec)
		}
		if pm.SampleCount() >= trainThreshold {
			if err := pm.Train(); err == nil {
				profile := pm.GetProfile()
				if profile != nil {
					data, _ := json.Marshal(profile)
					app.store.SaveProfileBlob(context.Background(), "default", string(data))
				}
				log.Printf("profil otomatik egitildi (%d ornek)", pm.SampleCount())
			}
		}
		app.updateTrayIcon()
		return
	}

	if vec == nil {
		return
	}

	normalized := pm.Normalize(vec)

	result, err := pm.Predict(normalized)
	if err != nil {
		return
	}

	if result.IsAnomaly {
		log.Printf("ANOMALY: score=%.4f level=%s", result.AnomalyScore, result.RiskLevel)
		app.sendNotification(result)
	}

	app.updateTrayIcon()
}

func (app *App) sendNotification(result *ml.RiskResult) {
	switch result.RiskLevel {
	case "high":
		app.tray.SetTooltip("YUKSEK RISK - Anormal davranis")
		notifyWin32("BehaviorDNA Uyarisi", "Anormal davranis tespit edildi. Risk seviyesi: Yuksek")
	case "critical":
		app.tray.SetTooltip("KRITIK RISK - Anormal davranis")
		notifyWin32("BehaviorDNA Uyarisi", "KRITIK: Davranis profilinizden ciddi sapma tespit edildi!")
	default:
		app.tray.SetTooltip("BehaviorDNA - Izleme Modu")
	}
}

func doTrain(app *App) {
	pm := app.profileMgr
	if pm.SampleCount() < trainThreshold {
		log.Printf("yeterli ornek yok: %d/%d", pm.SampleCount(), trainThreshold)
		return
	}

	if err := pm.Train(); err != nil {
		log.Printf("train error: %v", err)
		return
	}

	profile := pm.GetProfile()
	if profile != nil {
		data, _ := json.Marshal(profile)
		app.store.SaveProfileBlob(context.Background(), "default", string(data))
	}

	app.updateTrayIcon()
	log.Println("egitim tamamlandi")
}

func doReset(app *App) {
	app.profileMgr.Reset()
	app.store.SaveProfileBlob(context.Background(), "default", "")
	app.updateTrayIcon()
	log.Println("profil sifirlandi")
}

func loadProfile(app *App) {
	data, err := app.store.GetProfileBlob(context.Background(), "default")
	if err != nil || data == "" {
		log.Println("profil bulunamadi, ogrenme modu")
		app.updateTrayIcon()
		return
	}

	var profile ml.BehavioralProfile
	if err := json.Unmarshal([]byte(data), &profile); err != nil {
		log.Printf("profil yuklenemedi: %v", err)
		return
	}

	app.profileMgr.LoadProfile(&profile)
	log.Printf("profil yuklendi: %d ornek", profile.SampleCount)
	app.updateTrayIcon()
}

func (app *App) updateTrayIcon() {
	pm := app.profileMgr
	profile := pm.GetProfile()

	if profile == nil || profile.Status != "ready" {
		app.tray.SetTooltip("BehaviorDNA - Ogrenme Modu (" + itoa(pm.SampleCount()) + "/30)")
		app.tray.SetIcon("yellow")
		return
	}

	app.tray.SetTooltip("BehaviorDNA - Izleme Modu")
	app.tray.SetIcon("green")
}

func shutdown(app *App) {
	log.Println("shutting down...")
	app.capturer.Stop()
	app.tray.Hide()
	log.Println("stopped")
}

func notifyWin32(title, body string) {
	exec.Command("powershell",
		"-Command",
		`[System.Windows.Forms.MessageBox]::Show('`+body+`', '`+title+`', 'OK', 'Warning')`,
	).Start()
}

func closeSafe(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
