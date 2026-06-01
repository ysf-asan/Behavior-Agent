package ml

import (
	"errors"
	"math"
	"time"
)

type BehavioralProfile struct {
	UserID          string           `json:"userId"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
	SampleCount     int              `json:"sampleCount"`
	FeatureStats    *FeatureStats    `json:"featureStats,omitempty"`
	IsolationForest *IsolationForest `json:"isolationForest,omitempty"`
	Status          string           `json:"status"`
}

type FeatureStats struct {
	Means   []float64 `json:"means"`
	StdDevs []float64 `json:"stdDevs"`
	Count   int       `json:"count"`
}

type RiskResult struct {
	AnomalyScore float64 `json:"anomalyScore"`
	RiskLevel    string  `json:"riskLevel"`
	RawScore     float64 `json:"rawScore"`
	IsAnomaly    bool    `json:"isAnomaly"`
}

type ProfileManager struct {
	profile  *BehavioralProfile
	features [][]float64
}

func NewProfileManager() *ProfileManager {
	return &ProfileManager{
		features: make([][]float64, 0),
	}
}

func (pm *ProfileManager) LoadProfile(p *BehavioralProfile) {
	pm.profile = p
}

func (pm *ProfileManager) GetProfile() *BehavioralProfile {
	return pm.profile
}

func (pm *ProfileManager) AddSample(features []float64) {
	pm.features = append(pm.features, features)
}

func (pm *ProfileManager) SampleCount() int {
	return len(pm.features)
}

func (pm *ProfileManager) Train() error {
	if len(pm.features) < 30 {
		return errors.New("insufficient samples: need at least 30")
	}

	numFeatures := len(pm.features[0])
	numSamples := len(pm.features)

	means := make([]float64, numFeatures)
	for i := 0; i < numFeatures; i++ {
		var sum float64
		for _, s := range pm.features {
			sum += s[i]
		}
		means[i] = sum / float64(numSamples)
	}

	stdDevs := make([]float64, numFeatures)
	for i := 0; i < numFeatures; i++ {
		var sumSq float64
		for _, s := range pm.features {
			diff := s[i] - means[i]
			sumSq += diff * diff
		}
		stdDevs[i] = math.Sqrt(sumSq / float64(numSamples))
	}

	forest := NewIsolationForest(100, 256)
	forest.Fit(pm.features)

	if pm.profile == nil {
		pm.profile = &BehavioralProfile{
			CreatedAt: time.Now(),
		}
	}

	pm.profile.FeatureStats = &FeatureStats{
		Means:   means,
		StdDevs: stdDevs,
		Count:   numSamples,
	}
	pm.profile.IsolationForest = forest
	pm.profile.SampleCount = numSamples
	pm.profile.Status = "ready"
	pm.profile.UpdatedAt = time.Now()

	return nil
}

func (pm *ProfileManager) Predict(features []float64) (*RiskResult, error) {
	if pm.profile == nil || pm.profile.Status != "ready" || pm.profile.IsolationForest == nil {
		return nil, errors.New("profile not ready")
	}

	rawScore := pm.profile.IsolationForest.AnomalyScore(features)
	anomalyScore := 1.0 - 2.0*rawScore

	var riskLevel string
	switch {
	case anomalyScore >= 0.5:
		riskLevel = "low"
	case anomalyScore >= 0.0:
		riskLevel = "medium"
	case anomalyScore >= -0.5:
		riskLevel = "high"
	default:
		riskLevel = "critical"
	}

	return &RiskResult{
		AnomalyScore: anomalyScore,
		RiskLevel:    riskLevel,
		RawScore:     rawScore,
		IsAnomaly:    anomalyScore < 0.0,
	}, nil
}

func (pm *ProfileManager) Reset() {
	pm.profile = nil
	pm.features = make([][]float64, 0)
}

func (pm *ProfileManager) Normalize(features []float64) []float64 {
	if pm.profile == nil || pm.profile.FeatureStats == nil {
		result := make([]float64, len(features))
		copy(result, features)
		return result
	}

	result := make([]float64, len(features))
	for i := range features {
		if pm.profile.FeatureStats.StdDevs[i] == 0 {
			result[i] = 0
		} else {
			result[i] = (features[i] - pm.profile.FeatureStats.Means[i]) / pm.profile.FeatureStats.StdDevs[i]
		}
	}
	return result
}
