package qualityprobe

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"

	_ "embed"
)

const (
	fingerprintValueMin = 1
	fingerprintValueMax = 355
	fingerprintDim      = fingerprintValueMax - fingerprintValueMin + 1
	fingerprintAlpha    = 0.5
)

//go:embed data/unified_bank.json
var unifiedBankJSON []byte

var (
	fingerprintBankOnce sync.Once
	fingerprintBankVal  *fingerprintBank
	fingerprintBankErr  error
)

type fingerprintBank struct {
	Models []fingerprintModel `json:"models"`
	Robust struct {
		Hellinger     fingerprintHellinger `json:"hellinger"`
		OrderedBlocks fingerprintOrdered   `json:"ordered_blocks"`
	} `json:"robust"`
	Calibration map[string]fingerprintCalibration `json:"calibration"`
}

type fingerprintModel struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Family      string    `json:"family"`
	FamilyName  string    `json:"family_name"`
	Counts      []float64 `json:"counts"`
}

type fingerprintHellinger struct {
	FeatureMean   []float64   `json:"feature_mean"`
	FeatureScale  []float64   `json:"feature_scale"`
	NuisanceBasis [][]float64 `json:"nuisance_basis"`
	Centroids     [][]float64 `json:"centroids"`
}

type fingerprintOrdered struct {
	Weight               float64       `json:"weight"`
	FeatureMean          []float64     `json:"feature_mean"`
	FeatureScale         []float64     `json:"feature_scale"`
	NuisanceBasis        [][]float64   `json:"nuisance_basis"`
	Centroids            [][]float64   `json:"centroids"`
	EnvironmentCentroids [][][]float64 `json:"environment_centroids"`
}

type fingerprintCalibration struct {
	Beta       float64 `json:"beta"`
	CVAccuracy float64 `json:"cv_accuracy"`
}

// TraceOutput 是一次 ModelTrace 回答。
type TraceOutput struct {
	Text          string
	ExpectedCount int
}

// TraceResult 是指纹库归因结果。
type TraceResult struct {
	Prediction  string
	Probability float64
	UsedOutputs int
}

func loadFingerprintBank() (*fingerprintBank, error) {
	fingerprintBankOnce.Do(func() {
		var bank fingerprintBank
		if err := json.Unmarshal(unifiedBankJSON, &bank); err != nil {
			fingerprintBankErr = err
			return
		}
		if len(bank.Models) == 0 {
			fingerprintBankErr = fmt.Errorf("modeltrace bank has no models")
			return
		}
		fingerprintBankVal = &bank
	})
	return fingerprintBankVal, fingerprintBankErr
}

// AnalyzeTraceOutputs 用内置 unified_bank 对回答做 ModelTrace 归因。
// 算法与 xqy2006/ModelTrace 的 static/fingerprint-core.js 一致，指纹库 MIT 许可。
func AnalyzeTraceOutputs(outputs []TraceOutput) (TraceResult, error) {
	bank, err := loadFingerprintBank()
	if err != nil {
		return TraceResult{}, err
	}
	return analyzeTraceOutputs(outputs, bank)
}

func analyzeTraceOutputs(outputs []TraceOutput, bank *fingerprintBank) (TraceResult, error) {
	modelIDs := make([]string, len(bank.Models))
	for i, model := range bank.Models {
		modelIDs[i] = model.ID
	}
	type scored struct {
		counts []int
		scores []float64
	}
	valid := make([]scored, 0, len(outputs))
	for _, item := range outputs {
		numbers := parseFingerprintNumbers(item.Text)
		if len(numbers) < traceMinimumNumbers(item.ExpectedCount) {
			continue
		}
		valid = append(valid, scored{
			counts: countFingerprintNumbers(numbers),
			scores: robustScoreNumbers(numbers, bank),
		})
	}
	if len(valid) == 0 {
		return TraceResult{}, fmt.Errorf("没有可用的 ModelTrace 数字序列")
	}
	combined := make([]float64, len(modelIDs))
	for i := range combined {
		sum := 0.0
		for _, item := range valid {
			sum += item.scores[i]
		}
		combined[i] = sum / float64(len(valid))
	}
	key := strconv.Itoa(len(valid))
	if len(valid) > 3 {
		key = "3"
	}
	calib, ok := bank.Calibration[key]
	if !ok {
		return TraceResult{}, fmt.Errorf("modeltrace bank missing calibration %s", key)
	}
	scaled := make([]float64, len(combined))
	for i, value := range combined {
		scaled[i] = calib.Beta * value
	}
	probs := softmax(scaled)
	best := 0
	for i := 1; i < len(probs); i++ {
		if probs[i] > probs[best] {
			best = i
		}
	}
	return TraceResult{
		Prediction:  modelIDs[best],
		Probability: probs[best],
		UsedOutputs: len(valid),
	}, nil
}

// FingerprintMatchesGPT6 在归因结果属于 GPT-6 时返回 true。
func FingerprintMatchesGPT6(prediction string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(prediction)), "gpt-6-")
}

func parseFingerprintNumbers(text string) []int {
	matches := traceNumberPattern.FindAllStringIndex(text, -1)
	runs := make([][]int, 0)
	current := make([]int, 0)
	prevEnd := 0
	for _, loc := range matches {
		if len(current) > 0 && containsLetter(text[prevEnd:loc[0]]) {
			runs = append(runs, current)
			current = make([]int, 0)
		}
		value, err := strconv.Atoi(text[loc[0]:loc[1]])
		if err == nil && value >= fingerprintValueMin && value <= fingerprintValueMax {
			current = append(current, value)
		}
		prevEnd = loc[1]
	}
	if len(current) > 0 {
		runs = append(runs, current)
	}
	best := []int{}
	for _, run := range runs {
		if len(run) > len(best) {
			best = run
		}
	}
	return best
}

func containsLetter(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func countFingerprintNumbers(numbers []int) []int {
	counts := make([]int, fingerprintDim)
	for _, n := range numbers {
		counts[n-fingerprintValueMin]++
	}
	return counts
}

func hellingerFeature(counts []int) []float64 {
	total := fingerprintAlpha * float64(fingerprintDim)
	for _, value := range counts {
		total += float64(value)
	}
	out := make([]float64, len(counts))
	for i, value := range counts {
		out[i] = math.Sqrt((float64(value) + fingerprintAlpha) / total)
	}
	return out
}

func robustScoreCounts(counts []int, bank *fingerprintBank) []float64 {
	artifact := bank.Robust.Hellinger
	feature := hellingerFeature(counts)
	projected := make([]float64, len(feature))
	for i, value := range feature {
		projected[i] = (value - artifact.FeatureMean[i]) / artifact.FeatureScale[i]
	}
	projected = subtractBasis(projected, artifact.NuisanceBasis)
	projected = normalized(projected)
	scores := make([]float64, len(artifact.Centroids))
	for i, centroid := range artifact.Centroids {
		scores[i] = dot(projected, centroid)
	}
	return standardize(scores)
}

func orderedBlockFeature(numbers []int) []float64 {
	pieces := make([]float64, 0, 74)
	for _, chunk := range splitIntoFour(numbers) {
		bins := make([]float64, 16)
		for i := range bins {
			bins[i] = 0.5
		}
		for _, value := range chunk {
			idx := int(math.Floor(((float64(value) - 1) / 355) * 16))
			if idx > 15 {
				idx = 15
			}
			if idx < 0 {
				idx = 0
			}
			bins[idx]++
		}
		total := 0.0
		for _, value := range bins {
			total += value
		}
		for _, value := range bins {
			pieces = append(pieces, math.Sqrt(value/total))
		}
	}
	lastDigits := make([]float64, 10)
	for i := range lastDigits {
		lastDigits[i] = 0.5
	}
	for _, value := range numbers {
		lastDigits[value%10]++
	}
	total := 0.0
	for _, value := range lastDigits {
		total += value
	}
	for _, value := range lastDigits {
		pieces = append(pieces, math.Sqrt(value/total))
	}
	return pieces
}

func orderedBlockScores(numbers []int, bank *fingerprintBank) []float64 {
	artifact := bank.Robust.OrderedBlocks
	feature := orderedBlockFeature(numbers)
	standardizedFeature := make([]float64, len(feature))
	for i, value := range feature {
		standardizedFeature[i] = (value - artifact.FeatureMean[i]) / artifact.FeatureScale[i]
	}
	unit := normalized(standardizedFeature)
	envScores := make([][]float64, len(artifact.EnvironmentCentroids))
	for i, centroids := range artifact.EnvironmentCentroids {
		row := make([]float64, len(centroids))
		for j, centroid := range centroids {
			row[j] = dot(unit, centroid)
		}
		envScores[i] = row
	}
	templateRaw := make([]float64, len(artifact.Centroids))
	for modelIndex := range artifact.Centroids {
		best := math.Inf(-1)
		for _, scores := range envScores {
			if scores[modelIndex] > best {
				best = scores[modelIndex]
			}
		}
		templateRaw[modelIndex] = best
	}
	template := standardize(templateRaw)
	projected := normalized(subtractBasis(standardizedFeature, artifact.NuisanceBasis))
	nuisanceRaw := make([]float64, len(artifact.Centroids))
	for i, centroid := range artifact.Centroids {
		nuisanceRaw[i] = dot(projected, centroid)
	}
	nuisance := standardize(nuisanceRaw)
	fused := make([]float64, len(template))
	for i := range fused {
		fused[i] = 0.5*template[i] + 0.5*nuisance[i]
	}
	return standardize(fused)
}

func robustScoreNumbers(numbers []int, bank *fingerprintBank) []float64 {
	marginal := robustScoreCounts(countFingerprintNumbers(numbers), bank)
	artifact := bank.Robust.OrderedBlocks
	if artifact.Weight == 0 {
		return marginal
	}
	ordered := orderedBlockScores(numbers, bank)
	fused := make([]float64, len(marginal))
	for i := range fused {
		fused[i] = (1-artifact.Weight)*marginal[i] + artifact.Weight*ordered[i]
	}
	return fused
}

func splitIntoFour(values []int) [][]int {
	base := len(values) / 4
	remainder := len(values) % 4
	chunks := make([][]int, 4)
	start := 0
	for i := 0; i < 4; i++ {
		size := base
		if i < remainder {
			size++
		}
		chunks[i] = values[start : start+size]
		start += size
	}
	return chunks
}

func standardize(values []float64) []float64 {
	if len(values) == 0 {
		return values
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	center := sum / float64(len(values))
	varSum := 0.0
	for _, value := range values {
		d := value - center
		varSum += d * d
	}
	scale := math.Sqrt(varSum / float64(len(values)))
	if scale < 1e-12 {
		scale = 1e-12
	}
	out := make([]float64, len(values))
	for i, value := range values {
		out[i] = (value - center) / scale
	}
	return out
}

func dot(left, right []float64) float64 {
	n := len(left)
	if len(right) < n {
		n = len(right)
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += left[i] * right[i]
	}
	return sum
}

func norm(values []float64) float64 {
	return math.Sqrt(dot(values, values))
}

func normalized(values []float64) []float64 {
	scale := norm(values)
	if scale < 1e-12 {
		scale = 1e-12
	}
	out := make([]float64, len(values))
	for i, value := range values {
		out[i] = value / scale
	}
	return out
}

func subtractBasis(values []float64, basis [][]float64) []float64 {
	out := append([]float64(nil), values...)
	for _, vector := range basis {
		projection := dot(out, vector)
		for i := range out {
			out[i] -= projection * vector[i]
		}
	}
	return out
}

func softmax(values []float64) []float64 {
	if len(values) == 0 {
		return values
	}
	max := values[0]
	for _, value := range values[1:] {
		if value > max {
			max = value
		}
	}
	weights := make([]float64, len(values))
	total := 0.0
	for i, value := range values {
		w := math.Exp(value - max)
		weights[i] = w
		total += w
	}
	for i := range weights {
		weights[i] /= total
	}
	return weights
}
