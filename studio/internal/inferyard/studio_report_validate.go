package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"slices"
)

func validateStudioFacets(raw json.RawMessage) error {
	facets, err := studioObject(raw)
	if err != nil {
		return err
	}
	if err := studioClosed(facets, "bundle_label"); err != nil {
		return err
	}
	_, err = studioText(facets["bundle_label"])
	return err
}

func validateStudioProfile(raw json.RawMessage) error {
	profile, err := studioObject(raw)
	if err != nil {
		return err
	}
	if err := studioClosed(profile, "title", "engine", "machine"); err != nil {
		return err
	}
	if _, err := studioNullableText(profile["title"]); err != nil {
		return err
	}
	engine, err := studioObject(profile["engine"])
	if err != nil {
		return err
	}
	if err := studioClosed(engine, "binary_path", "adapter", "backend"); err != nil {
		return err
	}
	if _, err := studioNullableText(engine["binary_path"]); err != nil {
		return err
	}
	if _, err := studioText(engine["adapter"]); err != nil {
		return err
	}
	backend, err := studioText(engine["backend"])
	if err != nil || !slices.Contains(studioBackends, backend) {
		return errors.New("invalid studio report backend")
	}
	machine, err := studioObject(profile["machine"])
	if err != nil {
		return err
	}
	if err := studioClosed(machine, "cpu", "logical_cpus", "memory_total_bytes"); err != nil {
		return err
	}
	if _, err := studioNullableText(machine["cpu"]); err != nil {
		return err
	}
	if _, err := studioNullableCount(machine["logical_cpus"]); err != nil {
		return err
	}
	_, err = studioNullableCount(machine["memory_total_bytes"])
	return err
}

func validateStudioSummary(raw json.RawMessage) error {
	summary, err := studioObject(raw)
	if err != nil {
		return err
	}
	if err := studioClosed(summary, "completion_rate", "stop_reason"); err != nil {
		return err
	}
	if _, err := studioRate(summary["completion_rate"]); err != nil {
		return err
	}
	_, err = studioText(summary["stop_reason"])
	return err
}

func validateStudioRequest(raw json.RawMessage) error {
	request, err := studioObject(raw)
	if err != nil {
		return err
	}
	if err := studioClosed(request, "case_id", "category_label", "state", "quality", "latency_ms"); err != nil {
		return err
	}
	caseID, err := studioText(request["case_id"])
	if err != nil || !studioIdentifier.MatchString(caseID) {
		return errors.New("invalid studio report case id")
	}
	if _, err := studioText(request["category_label"]); err != nil {
		return err
	}
	state, err := studioText(request["state"])
	if err != nil || !slices.Contains(studioStates, state) {
		return errors.New("invalid studio report request state")
	}
	quality, err := studioNullableText(request["quality"])
	if err != nil || quality != nil && !slices.Contains(studioQualities, *quality) {
		return errors.New("invalid studio report request quality")
	}
	_, err = studioNullableNumber(request["latency_ms"])
	return err
}

func validateStudioDashboard(raw json.RawMessage, requests int) error {
	dashboard, err := studioObject(raw)
	if err != nil {
		return err
	}
	if err := studioClosed(dashboard, "quality", "categories", "latency", "sampled_peak_rss_bytes", "sampled_peak_rss_reason", "sampled_minimum_available_bytes", "minimum_available_reason"); err != nil {
		return err
	}
	quality, err := studioObject(dashboard["quality"])
	if err != nil {
		return err
	}
	if err := studioClosed(quality, "numerator", "denominator", "excluded", "complete", "reason"); err != nil {
		return err
	}
	counts, err := studioCountValuesFrom(quality)
	if err != nil {
		return err
	}
	complete, err := studioBool(quality["complete"])
	if err != nil {
		return err
	}
	reason, err := studioNullableText(quality["reason"])
	if err != nil {
		return err
	}
	if (reason == nil) != complete {
		return errors.New("studio report quality reason must appear exactly when incomplete")
	}
	categories, err := studioArray(dashboard["categories"])
	if err != nil || len(categories) > 6 {
		return errors.New("invalid studio report categories")
	}
	ids := map[string]bool{}
	var sum studioCountValues
	for _, rawCategory := range categories {
		category, err := studioObject(rawCategory)
		if err != nil {
			return err
		}
		if err := studioClosed(category, "id", "label", "rate"); err != nil {
			return err
		}
		id, err := studioText(category["id"])
		if err != nil || !slices.Contains(studioCategoryIDs, id) || ids[id] {
			return errors.New("invalid studio report category")
		}
		ids[id] = true
		if _, err := studioText(category["label"]); err != nil {
			return err
		}
		rate, err := studioRate(category["rate"])
		if err != nil {
			return err
		}
		sum.add(rate)
	}
	if counts != sum {
		return errors.New("studio report quality must equal the sum of category counts")
	}
	sampleCount, excluded, err := studioLatencyCounts(dashboard["latency"])
	if err != nil {
		return err
	}
	if sampleCount+excluded != int64(requests) {
		return errors.New("studio report latency must cover every request")
	}
	if err := validateStudioLatency(dashboard["latency"], sampleCount, excluded); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{"sampled_peak_rss_bytes", "sampled_peak_rss_reason"},
		{"sampled_minimum_available_bytes", "minimum_available_reason"},
	} {
		value, err := studioNullableNumber(dashboard[pair[0]])
		if err != nil {
			return err
		}
		reason, err := studioNullableText(dashboard[pair[1]])
		if err != nil {
			return err
		}
		if (value == nil) != (reason != nil) {
			return errors.New("studio report resource reason must appear exactly when the value is missing")
		}
	}
	return nil
}

type studioCountValues struct {
	numerator, denominator, excluded int64
}

func (c *studioCountValues) add(other studioCountValues) {
	c.numerator += other.numerator
	c.denominator += other.denominator
	c.excluded += other.excluded
}

func studioRate(raw json.RawMessage) (studioCountValues, error) {
	obj, err := studioObject(raw)
	if err != nil {
		return studioCountValues{}, err
	}
	if err := studioClosed(obj, "numerator", "denominator", "excluded"); err != nil {
		return studioCountValues{}, err
	}
	return studioCountValuesFrom(obj)
}

func studioCountValuesFrom(obj map[string]json.RawMessage) (studioCountValues, error) {
	var counts studioCountValues
	var err error
	if counts.numerator, err = studioCount(obj["numerator"]); err != nil {
		return counts, err
	}
	if counts.denominator, err = studioCount(obj["denominator"]); err != nil {
		return counts, err
	}
	if counts.excluded, err = studioCount(obj["excluded"]); err != nil {
		return counts, err
	}
	if counts.numerator > counts.denominator {
		return counts, errors.New("studio report numerator exceeds denominator")
	}
	return counts, nil
}

func studioLatencyCounts(raw json.RawMessage) (int64, int64, error) {
	latency, err := studioObject(raw)
	if err != nil {
		return 0, 0, err
	}
	if err := studioClosed(latency, "sample_count", "excluded", "min", "p50", "p95", "max"); err != nil {
		return 0, 0, err
	}
	sampleCount, err := studioCount(latency["sample_count"])
	if err != nil {
		return 0, 0, err
	}
	excluded, err := studioCount(latency["excluded"])
	return sampleCount, excluded, err
}

func validateStudioLatency(raw json.RawMessage, sampleCount, excluded int64) error {
	latency, err := studioObject(raw)
	if err != nil {
		return err
	}
	quantiles := map[string]*float64{}
	for _, key := range []string{"min", "p50", "p95", "max"} {
		value, err := studioNullableNumber(latency[key])
		if err != nil {
			return err
		}
		quantiles[key] = value
	}
	if sampleCount == 0 {
		for _, value := range quantiles {
			if value != nil {
				return errors.New("studio report empty latency requires null quantiles")
			}
		}
		return nil
	}
	if quantiles["min"] == nil || quantiles["p50"] == nil || quantiles["max"] == nil {
		return errors.New("studio report latency requires min, p50 and max")
	}
	if (quantiles["p95"] == nil) != (sampleCount < 20) {
		return errors.New("studio report p95 is required from twenty samples")
	}
	if !(*quantiles["min"] <= *quantiles["p50"] && *quantiles["p50"] <= *quantiles["max"]) {
		return errors.New("studio report latency quantiles are not ordered")
	}
	if quantiles["p95"] != nil && !(*quantiles["p50"] <= *quantiles["p95"] && *quantiles["p95"] <= *quantiles["max"]) {
		return errors.New("studio report latency quantiles are not ordered")
	}
	return nil
}

func studioObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("invalid studio report object")
	}
	return obj, nil
}

func studioArray(raw json.RawMessage) ([]json.RawMessage, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || arr == nil {
		return nil, errors.New("invalid studio report array")
	}
	return arr, nil
}

// studioClosed enforces required presence and rejects unknown keys, matching
// the schema's additionalProperties:false. Presence is checked separately from
// nullability so explicit null stays meaningful for nullable fields.
func studioClosed(obj map[string]json.RawMessage, required ...string) error {
	for _, name := range required {
		if _, ok := obj[name]; !ok {
			return errors.New("missing studio report field")
		}
	}
	if len(obj) != len(required) {
		return errors.New("unknown studio report field")
	}
	return nil
}

func studioNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func studioText(raw json.RawMessage) (string, error) {
	if studioNull(raw) {
		return "", errors.New("invalid studio report value")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return "", errors.New("invalid studio report value")
	}
	return value, nil
}

func studioNullableText(raw json.RawMessage) (*string, error) {
	if studioNull(raw) {
		return nil, nil
	}
	value, err := studioText(raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func studioBool(raw json.RawMessage) (bool, error) {
	if studioNull(raw) {
		return false, errors.New("invalid studio report value")
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, errors.New("invalid studio report value")
	}
	return value, nil
}

func studioCount(raw json.RawMessage) (int64, error) {
	if studioNull(raw) {
		return 0, errors.New("invalid studio report count")
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil || value < 0 {
		return 0, errors.New("invalid studio report count")
	}
	return value, nil
}

func studioNullableCount(raw json.RawMessage) (*int64, error) {
	if studioNull(raw) {
		return nil, nil
	}
	value, err := studioCount(raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func studioNullableNumber(raw json.RawMessage) (*float64, error) {
	if studioNull(raw) {
		return nil, nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, errors.New("invalid studio report number")
	}
	return &value, nil
}
