package compression

// SmartCrusherLite — statistical JSON compression inspired by Headroom's SmartCrusher.
// Pure Go, no ML, no external deps. Key concepts:
// 1. Field importance scoring (drop verbose low-value fields)
// 2. Array truncation (large arrays → head + tail)
// 3. Value summarization (long strings → truncated)
// 4. Outlier-aware numeric handling (keep representative values)

import (
	"encoding/json"
	"math"
	"strings"
)

// fieldImportance scores a JSON field name for importance (0.0 = drop, 1.0 = keep).
// Lower score = more likely to be verbose/low-value.
func fieldImportance(name string) float64 {
	lower := strings.ToLower(name)

	// High importance — structural/identity fields (keep these)
	high := []string{
		"id", "name", "type", "status", "error", "message", "code",
		"key", "value", "label", "title", "description", "role",
		"method", "path", "url", "endpoint", "action", "result",
	}
	for _, h := range high {
		if lower == h {
			return 0.9
		}
	}

	// Medium importance — useful but often verbose
	medium := []string{
		"content", "body", "data", "payload", "response", "request",
		"output", "input", "params", "args", "config", "options",
		"metadata", "attributes", "properties", "fields", "items",
		"created_at", "updated_at", "timestamp", "time",
	}
	for _, m := range medium {
		if lower == m {
			return 0.6
		}
	}

	// Low importance — verbose/debug fields (candidates for dropping)
	low := []string{
		"stacktrace", "stack_trace", "traceback", "backtrace",
		"debug", "verbose", "raw", "full_response", "original",
		"request_body", "response_body", "html", "content_html",
		"rendered", "preview", "snapshot", "dump", "log", "logs",
		"warnings", "deprecations", "suggestions", "hints",
		"debug_info", "internal", "_debug", "_raw", "_original",
	}
	for _, l := range low {
		if lower == l {
			return 0.2
		}
	}

	// Unknown fields — moderate importance (don't drop blindly)
	return 0.5
}

// SmartCrushJSON compresses a JSON string using statistical strategies.
// Returns compressed JSON or original if no savings.
func SmartCrushJSON(text string, targetRatio float64) string {
	if len(text) < 100 {
		return text
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return text // Not valid JSON
	}

	crushed := crushValue(parsed, targetRatio)

	compact, err := json.Marshal(crushed)
	if err != nil {
		return text
	}

	// Only return if we actually saved space
	if len(compact) < len(text) {
		return string(compact)
	}
	return text
}

// crushValue dispatches to type-specific crushers.
func crushValue(v interface{}, targetRatio float64) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return crushObject(val, targetRatio)
	case []interface{}:
		return crushArray(val, targetRatio)
	default:
		return v
	}
}

// crushObject compresses a JSON object by field importance.
func crushObject(obj map[string]interface{}, targetRatio float64) map[string]interface{} {
	if len(obj) == 0 {
		return obj
	}

	// Score all fields
	type scoredField struct {
		key   string
		value interface{}
		score float64
		size  int
	}

	fields := make([]scoredField, 0, len(obj))
	totalSize := 0
	for k, v := range obj {
		// Estimate field size
		b, _ := json.Marshal(v)
		size := len(k) + len(b) + 4 // key + value + quotes + colon
		totalSize += size
		fields = append(fields, scoredField{
			key:   k,
			value: v,
			score: fieldImportance(k),
			size:  size,
		})
	}

	// If already small enough, just recurse into children
	if float64(totalSize) < 500 {
		result := make(map[string]interface{}, len(obj))
		for _, f := range fields {
			result[f.key] = crushValue(f.value, targetRatio)
		}
		return result
	}

	// Sort by score ascending (lowest importance first)
	for i := 0; i < len(fields); i++ {
		for j := i + 1; j < len(fields); j++ {
			if fields[j].score < fields[i].score {
				fields[i], fields[j] = fields[j], fields[i]
			}
		}
	}

	// Greedy drop: drop lowest-scored fields until we hit target ratio
	result := make(map[string]interface{})
	savedBytes := 0
	for _, f := range fields {
		result[f.key] = crushValue(f.value, targetRatio)
	}

	// Second pass: drop low-importance fields if still too big
	resultBytes, _ := json.Marshal(result)
	if float64(len(resultBytes))/float64(totalSize) > targetRatio {
		// Need to drop more — rebuild without lowest-scored fields
		result = make(map[string]interface{})
		kept := 0
		for _, f := range fields {
			if f.score < 0.3 && kept > len(fields)/2 {
				savedBytes += f.size
				continue // Drop this field
			}
			result[f.key] = crushValue(f.value, targetRatio)
			kept++
		}
	}

	_ = savedBytes
	return result
}

// crushArray compresses JSON arrays using adaptive strategies.
func crushArray(arr []interface{}, targetRatio float64) interface{} {
	if len(arr) == 0 {
		return arr
	}

	// Small arrays: just recurse into children
	if len(arr) <= 10 {
		result := make([]interface{}, len(arr))
		for i, v := range arr {
			result[i] = crushValue(v, targetRatio)
		}
		return result
	}

	// Classify array by element types
	arrayType := classifyJSONArray(arr)

	switch arrayType {
	case jsonArrayDict:
		return crushDictArray(arr, targetRatio)
	case jsonArrayString:
		return crushStringArray(arr, targetRatio)
	case jsonArrayNumber:
		return crushNumberArray(arr, targetRatio)
	default:
		// Mixed or nested: just truncate
		return truncateArray(arr, targetRatio)
	}
}

// jsonArrayType represents the type of elements in a JSON array.
type jsonArrayType int

const (
	jsonArrayDict   jsonArrayType = iota // [{...}, {...}, ...]
	jsonArrayString                      // ["a", "b", ...]
	jsonArrayNumber                      // [1, 2, ...]
	jsonArrayMixed                       // heterogeneous
)

// classifyJSONArray determines the dominant type of array elements.
func classifyJSONArray(arr []interface{}) jsonArrayType {
	if len(arr) == 0 {
		return jsonArrayMixed
	}

	counts := map[jsonArrayType]int{}
	for _, v := range arr {
		switch v.(type) {
		case map[string]interface{}:
			counts[jsonArrayDict]++
		case string:
			counts[jsonArrayString]++
		case float64:
			counts[jsonArrayNumber]++
		}
	}

	// Need >70% dominance to classify
	threshold := int(float64(len(arr)) * 0.7)
	for t, c := range counts {
		if c >= threshold {
			return t
		}
	}
	return jsonArrayMixed
}

// crushDictArray compresses an array of objects using field importance + sampling.
func crushDictArray(arr []interface{}, targetRatio float64) []interface{} {
	// Step 1: Merge all field names and compute average importance
	fieldScores := map[string]float64{}
	fieldCounts := map[string]int{}
	for _, v := range arr {
		if obj, ok := v.(map[string]interface{}); ok {
			for k := range obj {
				fieldScores[k] += fieldImportance(k)
				fieldCounts[k]++
			}
		}
	}

	// Average importance per field
	for k := range fieldScores {
		fieldScores[k] /= float64(fieldCounts[k])
	}

	// Step 2: Identify fields to drop (score < 0.3 and present in < 50% of items)
	dropFields := map[string]bool{}
	for k, score := range fieldScores {
		if score < 0.3 && float64(fieldCounts[k])/float64(len(arr)) < 0.5 {
			dropFields[k] = true
		}
	}

	// Step 3: If still too big, truncate array (keep head + tail)
	result := make([]interface{}, 0, len(arr))
	for _, v := range arr {
		if obj, ok := v.(map[string]interface{}); ok {
			compressed := make(map[string]interface{})
			for k, val := range obj {
				if dropFields[k] {
					continue
				}
				compressed[k] = crushValue(val, targetRatio)
			}
			result = append(result, compressed)
		} else {
			result = append(result, crushValue(v, targetRatio))
		}
	}

	// Truncate if still too large
	resultBytes, _ := json.Marshal(result)
	arrBytes, _ := json.Marshal(arr)
	if float64(len(resultBytes))/float64(len(arrBytes)) > targetRatio && len(result) > 10 {
		result = truncateArray(result, targetRatio)
	}

	return result
}

// crushStringArray compresses a string array by dedup + truncation.
func crushStringArray(arr []interface{}, targetRatio float64) []interface{} {
	// Dedup
	seen := map[string]bool{}
	unique := make([]interface{}, 0, len(arr))
	for _, v := range arr {
		s, _ := v.(string)
		if !seen[s] {
			seen[s] = true
			unique = append(unique, v)
		}
	}

	// Truncate if still large
	if len(unique) > 20 {
		return truncateArray(unique, targetRatio)
	}
	return unique
}

// crushNumberArray compresses a number array by keeping statistical summary.
func crushNumberArray(arr []interface{}, targetRatio float64) []interface{} {
	if len(arr) <= 10 {
		return arr
	}

	// Compute stats
	nums := make([]float64, 0, len(arr))
	for _, v := range arr {
		if n, ok := v.(float64); ok {
			nums = append(nums, n)
		}
	}

	if len(nums) == 0 {
		return arr
	}

	min, max, mean := nums[0], nums[0], 0.0
	for _, n := range nums {
		if n < min {
			min = n
		}
		if n > max {
			max = n
		}
		mean += n
	}
	mean /= float64(len(nums))

	// If values are all similar (low variance), just keep summary
	variance := 0.0
	for _, n := range nums {
		variance += (n - mean) * (n - mean)
	}
	variance /= float64(len(nums))
	stddev := math.Sqrt(variance)

	// If stddev < 10% of mean, values are similar — keep summary
	if stddev < math.Abs(mean)*0.1 && len(arr) > 20 {
		return []interface{}{
			map[string]interface{}{
				"summary": true,
				"count":   len(arr),
				"min":     min,
				"max":     max,
				"mean":    math.Round(mean*100) / 100,
			},
		}
	}

	// Otherwise truncate
	return truncateArray(arr, targetRatio)
}

// truncateArray keeps head + tail items, replacing middle with a count marker.
func truncateArray(arr []interface{}, targetRatio float64) []interface{} {
	targetLen := max(6, int(float64(len(arr))*targetRatio))
	if targetLen >= len(arr) {
		return arr
	}

	headLen := targetLen / 2
	tailLen := targetLen - headLen

	result := make([]interface{}, 0, targetLen+1)
	result = append(result, arr[:headLen]...)
	result = append(result, map[string]interface{}{
		"_truncated": true,
		"_omitted":   len(arr) - headLen - tailLen,
		"_total":     len(arr),
	})
	result = append(result, arr[len(arr)-tailLen:]...)

	return result
}
