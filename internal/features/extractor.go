package features

import (
	"math"
	"sync"
)

type RawEvent struct {
	Type      string
	Timestamp int64
	Data      map[string]interface{}
}

type FeatureVector []float64

type Extractor struct {
	keystrokeBuf []RawEvent
	mouseBuf     []RawEvent
	scrollBuf    []RawEvent
	clickBuf     []RawEvent
	mu           sync.Mutex
}

func NewExtractor() *Extractor {
	return &Extractor{}
}

func (e *Extractor) Add(event RawEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch event.Type {
	case "keystroke":
		e.keystrokeBuf = append(e.keystrokeBuf, event)
	case "mouse":
		e.mouseBuf = append(e.mouseBuf, event)
	case "scroll":
		e.scrollBuf = append(e.scrollBuf, event)
	case "click":
		e.clickBuf = append(e.clickBuf, event)
	}
}

func (e *Extractor) Flush() FeatureVector {
	e.mu.Lock()
	defer e.mu.Unlock()
	features := make(FeatureVector, 44)

	keystroke := e.keystrokeBuf
	mouse := e.mouseBuf
	scroll := e.scrollBuf
	click := e.clickBuf
	e.keystrokeBuf = nil
	e.mouseBuf = nil
	e.scrollBuf = nil
	e.clickBuf = nil

	dwellMean, dwellStd, dwellMin, dwellMax, dwellMedian, dwellCount := computeDwell(keystroke)
	flightMean, flightStd, flightMin, flightMax, flightMedian, flightCount := computeFlight(keystroke)
	latencyMean, latencyStd, latencyMin, latencyMax, latencyMedian, latencyCount := computeLatency(keystroke)

	features[0] = dwellMean
	features[1] = dwellStd
	features[2] = dwellMin
	features[3] = dwellMax
	features[4] = dwellMedian
	features[5] = dwellCount
	features[6] = flightMean
	features[7] = flightStd
	features[8] = flightMin
	features[9] = flightMax
	features[10] = flightMedian
	features[11] = flightCount
	features[12] = latencyMean
	features[13] = latencyStd
	features[14] = latencyMin
	features[15] = latencyMax
	features[16] = latencyMedian
	features[17] = latencyCount

	speedMean, speedStd, speedCount := computeMouseSpeed(mouse)
	accelMean, accelStd, accelCount := computeMouseAccel(mouse)
	angleMean, angleStd, angleCount := computeMouseAngle(mouse)
	distMean, distStd := computeMouseDist(mouse)

	features[18] = speedMean
	features[19] = speedStd
	features[20] = speedCount
	features[21] = accelMean
	features[22] = accelStd
	features[23] = accelCount
	features[24] = angleMean
	features[25] = angleStd
	features[26] = angleCount
	features[27] = distMean
	features[28] = distStd
	features[29] = float64(len(mouse))

	scrollSpeedMean, scrollSpeedStd := computeScrollSpeedStats(scroll)
	scrollTimeMean, scrollTimeStd := computeScrollTimeStats(scroll)
	dirRev := computeDirReversals(scroll)
	totalScrollDist := computeTotalScrollDist(scroll)
	scrollCount := float64(len(scroll))
	scrollIdle := computeScrollIdle(scroll)

	features[30] = scrollSpeedMean
	features[31] = scrollSpeedStd
	features[32] = scrollTimeMean
	features[33] = scrollTimeStd
	features[34] = dirRev
	features[35] = totalScrollDist
	features[36] = scrollCount
	features[37] = scrollIdle

	clickIntervalMean, clickIntervalStd := computeClickInterval(click)
	clickVarX, clickVarY := computeClickPosVar(click)
	doubleClickRatio := computeDoubleClick(click)
	clickCount := float64(len(click))

	features[38] = clickIntervalMean
	features[39] = clickIntervalStd
	features[40] = clickVarX
	features[41] = clickVarY
	features[42] = doubleClickRatio
	features[43] = clickCount

	return features
}

func getString(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getFloat(data map[string]interface{}, key string) float64 {
	if v, ok := data[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func computeStats(values []float64) (mean, std, min, max, median, count float64) {
	n := len(values)
	if n == 0 {
		return 0, 0, 0, 0, 0, 0
	}
	count = float64(n)

	sorted := make([]float64, n)
	copy(sorted, values)
	insertionSort(sorted)

	min = sorted[0]
	max = sorted[n-1]

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean = sum / count

	var varSum float64
	for _, v := range sorted {
		d := v - mean
		varSum += d * d
	}
	std = math.Sqrt(varSum / count)

	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	} else {
		median = sorted[n/2]
	}
	return
}

func insertionSort(a []float64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func computeDwell(events []RawEvent) (mean, std, min, max, median, count float64) {
	type keyDown struct {
		key string
		ts  int64
	}
	var pending []keyDown
	var dwells []float64

	for _, e := range events {
		key := getString(e.Data, "key")
		state := getString(e.Data, "state")
		if state == "down" {
			pending = append(pending, keyDown{key, e.Timestamp})
		} else if state == "up" {
			for i, p := range pending {
				if p.key == key {
					dwell := float64(e.Timestamp-p.ts) / 1000
					dwells = append(dwells, dwell)
					pending = append(pending[:i], pending[i+1:]...)
					break
				}
			}
		}
	}
	return computeStats(dwells)
}

func computeFlight(events []RawEvent) (mean, std, min, max, median, count float64) {
	var flights []float64
	var lastUp int64 = -1

	for _, e := range events {
		state := getString(e.Data, "state")
		if state == "up" {
			lastUp = e.Timestamp
		} else if state == "down" && lastUp != -1 {
			flight := float64(e.Timestamp-lastUp) / 1000
			flights = append(flights, flight)
			lastUp = -1
		}
	}
	return computeStats(flights)
}

func computeLatency(events []RawEvent) (mean, std, min, max, median, count float64) {
	var latencies []float64
	var lastKey string
	var lastTime int64

	for _, e := range events {
		key := getString(e.Data, "key")
		state := getString(e.Data, "state")
		if state == "down" {
			if lastTime != 0 && key != lastKey {
				lat := float64(e.Timestamp-lastTime) / 1000
				latencies = append(latencies, lat)
			}
			lastKey = key
			lastTime = e.Timestamp
		}
	}
	return computeStats(latencies)
}

func computeMouseSpeed(events []RawEvent) (mean, std, count float64) {
	if len(events) < 2 {
		return 0, 0, 0
	}
	var speeds []float64
	for i := 1; i < len(events); i++ {
		x0 := getFloat(events[i-1].Data, "x")
		y0 := getFloat(events[i-1].Data, "y")
		t0 := events[i-1].Timestamp
		x1 := getFloat(events[i].Data, "x")
		y1 := getFloat(events[i].Data, "y")
		t1 := events[i].Timestamp
		dt := t1 - t0
		if dt <= 0 {
			continue
		}
		dx := x1 - x0
		dy := y1 - y0
		dist := math.Sqrt(dx*dx + dy*dy)
		speed := dist * 1000 / float64(dt)
		speeds = append(speeds, speed)
	}
	mean, std, _, _, _, count = computeStats(speeds)
	return
}

func computeMouseAccel(events []RawEvent) (mean, std, count float64) {
	if len(events) < 3 {
		return 0, 0, 0
	}
	var speeds []float64
	for i := 1; i < len(events); i++ {
		x0 := getFloat(events[i-1].Data, "x")
		y0 := getFloat(events[i-1].Data, "y")
		t0 := events[i-1].Timestamp
		x1 := getFloat(events[i].Data, "x")
		y1 := getFloat(events[i].Data, "y")
		t1 := events[i].Timestamp
		dt := t1 - t0
		if dt <= 0 {
			continue
		}
		dx := x1 - x0
		dy := y1 - y0
		dist := math.Sqrt(dx*dx + dy*dy)
		speed := dist * 1000 / float64(dt)
		speeds = append(speeds, speed)
	}
	if len(speeds) < 2 {
		return 0, 0, 0
	}
	var accels []float64
	for i := 1; i < len(speeds); i++ {
		accels = append(accels, speeds[i]-speeds[i-1])
	}
	mean, std, _, _, _, count = computeStats(accels)
	return
}

func computeMouseAngle(events []RawEvent) (mean, std, count float64) {
	if len(events) < 3 {
		return 0, 0, 0
	}
	var angles []float64
	for i := 1; i < len(events); i++ {
		x0 := getFloat(events[i-1].Data, "x")
		y0 := getFloat(events[i-1].Data, "y")
		x1 := getFloat(events[i].Data, "x")
		y1 := getFloat(events[i].Data, "y")
		dx := x1 - x0
		dy := y1 - y0
		if dx == 0 && dy == 0 {
			continue
		}
		angle := math.Atan2(dy, dx) * 180 / math.Pi
		angles = append(angles, angle)
	}
	if len(angles) < 2 {
		return 0, 0, 0
	}
	var diffs []float64
	for i := 1; i < len(angles); i++ {
		diff := angles[i] - angles[i-1]
		for diff > 180 {
			diff -= 360
		}
		for diff < -180 {
			diff += 360
		}
		diffs = append(diffs, diff)
	}
	mean, std, _, _, _, count = computeStats(diffs)
	return
}

func computeMouseDist(events []RawEvent) (mean, std float64) {
	if len(events) < 2 {
		return 0, 0
	}
	var dists []float64
	for i := 1; i < len(events); i++ {
		x0 := getFloat(events[i-1].Data, "x")
		y0 := getFloat(events[i-1].Data, "y")
		x1 := getFloat(events[i].Data, "x")
		y1 := getFloat(events[i].Data, "y")
		dx := x1 - x0
		dy := y1 - y0
		dist := math.Sqrt(dx*dx + dy*dy)
		dists = append(dists, dist)
	}
	mean, std, _, _, _, _ = computeStats(dists)
	return
}

func computeScrollSpeedStats(events []RawEvent) (mean, std float64) {
	if len(events) < 2 {
		return 0, 0
	}
	var speeds []float64
	for i := 1; i < len(events); i++ {
		delta := getFloat(events[i].Data, "delta")
		dt := events[i].Timestamp - events[i-1].Timestamp
		if dt <= 0 {
			continue
		}
		speed := math.Abs(delta) * 1000 / float64(dt)
		speeds = append(speeds, speed)
	}
	mean, std, _, _, _, _ = computeStats(speeds)
	return
}

func computeScrollTimeStats(events []RawEvent) (mean, std float64) {
	if len(events) < 2 {
		return 0, 0
	}
	var intervals []float64
	for i := 1; i < len(events); i++ {
		dt := events[i].Timestamp - events[i-1].Timestamp
		intervals = append(intervals, float64(dt)/1000)
	}
	mean, std, _, _, _, _ = computeStats(intervals)
	return
}

func computeDirReversals(events []RawEvent) float64 {
	if len(events) < 2 {
		return 0
	}
	rev := 0
	var prev float64
	hasPrev := false
	for _, e := range events {
		delta := getFloat(e.Data, "delta")
		if delta == 0 {
			continue
		}
		if hasPrev {
			if (delta > 0) != (prev > 0) {
				rev++
			}
		}
		prev = delta
		hasPrev = true
	}
	return float64(rev)
}

func computeTotalScrollDist(events []RawEvent) float64 {
	var total float64
	for _, e := range events {
		total += math.Abs(getFloat(e.Data, "delta"))
	}
	return total
}

func computeScrollIdle(events []RawEvent) float64 {
	n := len(events)
	if n < 2 {
		return 0
	}
	totalTime := float64(events[n-1].Timestamp - events[0].Timestamp)
	if totalTime <= 0 {
		return 0
	}
	var idleTime float64
	for i := 1; i < n; i++ {
		dt := events[i].Timestamp - events[i-1].Timestamp
		if dt > 500000 {
			idleTime += float64(dt)
		}
	}
	return idleTime / totalTime
}

func computeClickInterval(events []RawEvent) (mean, std float64) {
	n := len(events)
	if n < 2 {
		return 0, 0
	}
	var intervals []float64
	for i := 1; i < n; i++ {
		dt := events[i].Timestamp - events[i-1].Timestamp
		intervals = append(intervals, float64(dt)/1000)
	}
	mean, std, _, _, _, _ = computeStats(intervals)
	return
}

func computeClickPosVar(events []RawEvent) (varX, varY float64) {
	n := len(events)
	if n == 0 {
		return 0, 0
	}
	var meanX, meanY float64
	for _, e := range events {
		meanX += getFloat(e.Data, "x")
		meanY += getFloat(e.Data, "y")
	}
	meanX /= float64(n)
	meanY /= float64(n)

	var sumSqX, sumSqY float64
	for _, e := range events {
		dx := getFloat(e.Data, "x") - meanX
		dy := getFloat(e.Data, "y") - meanY
		sumSqX += dx * dx
		sumSqY += dy * dy
	}
	varX = sumSqX / float64(n)
	varY = sumSqY / float64(n)
	return
}

func computeDoubleClick(events []RawEvent) float64 {
	n := len(events)
	if n < 2 {
		return 0
	}
	fast := 0
	for i := 1; i < n; i++ {
		dt := events[i].Timestamp - events[i-1].Timestamp
		if dt < 300000 {
			fast++
		}
	}
	return float64(fast) / float64(n)
}
