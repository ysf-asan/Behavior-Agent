package ml

import (
	"encoding/json"
	"math"
	"math/rand"
)

var rng = rand.New(rand.NewSource(42))

type IsolationForest struct {
	Trees      []*ITree `json:"trees"`
	NumTrees   int      `json:"numTrees"`
	SampleSize int      `json:"sampleSize"`
}

type ITree struct {
	Root *ITreeNode `json:"root"`
}

type ITreeNode struct {
	Left       *ITreeNode `json:"left,omitempty"`
	Right      *ITreeNode `json:"right,omitempty"`
	SplitAtt   int        `json:"splitAtt,omitempty"`
	SplitValue float64    `json:"splitValue,omitempty"`
	Size       int        `json:"size"`
	Height     int        `json:"height"`
}

func NewIsolationForest(numTrees, sampleSize int) *IsolationForest {
	return &IsolationForest{
		Trees:      make([]*ITree, 0),
		NumTrees:   numTrees,
		SampleSize: sampleSize,
	}
}

func (f *IsolationForest) Fit(samples [][]float64) {
	n := len(samples)
	if n == 0 {
		return
	}

	sampleSize := f.SampleSize
	if sampleSize > n {
		sampleSize = n
	}

	limit := int(math.Ceil(math.Log2(float64(sampleSize))))

	f.Trees = make([]*ITree, f.NumTrees)
	for i := 0; i < f.NumTrees; i++ {
		subsample := make([][]float64, sampleSize)
		indices := rng.Perm(n)
		for j := 0; j < sampleSize; j++ {
			subsample[j] = samples[indices[j]]
		}
		root := buildITree(subsample, 0, limit)
		f.Trees[i] = &ITree{Root: root}
	}
}

func buildITree(samples [][]float64, depth, limit int) *ITreeNode {
	node := &ITreeNode{
		Size:   len(samples),
		Height: depth,
	}

	if depth >= limit || len(samples) <= 1 {
		return node
	}

	numFeatures := len(samples[0])
	if numFeatures == 0 {
		return node
	}

	splitAtt := rng.Intn(numFeatures)

	minVal := samples[0][splitAtt]
	maxVal := samples[0][splitAtt]
	for _, s := range samples[1:] {
		if s[splitAtt] < minVal {
			minVal = s[splitAtt]
		}
		if s[splitAtt] > maxVal {
			maxVal = s[splitAtt]
		}
	}

	if minVal == maxVal {
		return node
	}

	splitValue := minVal + rng.Float64()*(maxVal-minVal)

	var left, right [][]float64
	for _, s := range samples {
		if s[splitAtt] < splitValue {
			left = append(left, s)
		} else {
			right = append(right, s)
		}
	}

	if len(left) == 0 || len(right) == 0 {
		return node
	}

	node.SplitAtt = splitAtt
	node.SplitValue = splitValue
	node.Left = buildITree(left, depth+1, limit)
	node.Right = buildITree(right, depth+1, limit)

	return node
}

func (f *IsolationForest) AnomalyScore(sample []float64) float64 {
	if len(f.Trees) == 0 {
		return 0.5
	}

	var totalPath float64
	for _, tree := range f.Trees {
		totalPath += tree.pathLength(sample, 0)
	}
	avgPath := totalPath / float64(len(f.Trees))
	expectedPath := c(float64(f.SampleSize))
	if expectedPath == 0 {
		return 0.5
	}
	return math.Pow(2, -avgPath/expectedPath)
}

func c(n float64) float64 {
	if n <= 1 {
		return 0
	}
	h := math.Log(n-1) + 0.5772156649
	return 2*h - 2*(n-1)/n
}

func (t *ITree) pathLength(sample []float64, depth int) float64 {
	return t.Root.pathLength(sample, depth)
}

func (n *ITreeNode) pathLength(sample []float64, depth int) float64 {
	if n.Left == nil || n.Right == nil {
		if n.Size <= 1 {
			return float64(depth)
		}
		return float64(depth) + c(float64(n.Size))
	}
	if sample[n.SplitAtt] < n.SplitValue {
		return n.Left.pathLength(sample, depth+1)
	}
	return n.Right.pathLength(sample, depth+1)
}

func (f *IsolationForest) MarshalJSON() ([]byte, error) {
	type alias IsolationForest
	return json.Marshal((*alias)(f))
}

func (f *IsolationForest) UnmarshalJSON(data []byte) error {
	type alias IsolationForest
	return json.Unmarshal(data, (*alias)(f))
}
