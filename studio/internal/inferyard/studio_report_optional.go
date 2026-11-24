package inferyard

import (
	"encoding/json"
	"errors"
	"slices"
)

type studioIntervalValues struct {
	count     int64
	min       *float64
	p50       *float64
	p95       *float64
	max       *float64
	reason    *string
	p95Reason *string
}

func validateStudioThroughput(raw json.RawMessage) error {
	throughput, err := studioObject(raw)
	if err != nil {
		return err
	}
	if err := studioClosed(throughput, "requests_per_second", "requests_per_second_reason", "output_tokens_per_second", "output_tokens_per_second_reason", "goodput_per_second", "goodput_per_second_reason"); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{"requests_per_second", "requests_per_second_reason"},
		{"output_tokens_per_second", "output_tokens_per_second_reason"},
		{"goodput_per_second", "goodput_per_second_reason"},
	} {
		value, err := studioNullableNumber(throughput[pair[0]])
		if err != nil {
			return err
		}
		reason, err := studioNullableText(throughput[pair[1]])
		if err != nil {
			return err
		}
		if (value == nil) != (reason != nil) {
			return errors.New("studio report throughput reason must appear exactly when the value is missing")
		}
	}
	return nil
}

func validateStudioIntervals(raw json.RawMessage) error {
	intervals, err := studioIntervalValuesFrom(raw)
	if err != nil {
		return err
	}
	if intervals.count == 0 {
		return validateStudioEmptyIntervals(intervals)
	}
	return validateStudioMeasuredIntervals(intervals)
}

func studioIntervalValuesFrom(raw json.RawMessage) (studioIntervalValues, error) {
	var intervals studioIntervalValues
	obj, err := studioObject(raw)
	if err != nil {
		return intervals, err
	}
	if err := studioClosed(obj, "interval_count", "request_count", "min", "p50", "p95", "p95_reason", "max", "reason"); err != nil {
		return intervals, err
	}
	if intervals.count, err = studioCount(obj["interval_count"]); err != nil {
		return intervals, err
	}
	if _, err := studioCount(obj["request_count"]); err != nil {
		return intervals, err
	}
	quantiles := []struct {
		key   string
		value **float64
	}{
		{"min", &intervals.min},
		{"p50", &intervals.p50},
		{"p95", &intervals.p95},
		{"max", &intervals.max},
	}
	for _, quantile := range quantiles {
		if *quantile.value, err = studioNullableNumber(obj[quantile.key]); err != nil {
			return intervals, err
		}
	}
	if intervals.reason, err = studioNullableText(obj["reason"]); err != nil {
		return intervals, err
	}
	intervals.p95Reason, err = studioNullableText(obj["p95_reason"])
	return intervals, err
}

func validateStudioEmptyIntervals(intervals studioIntervalValues) error {
	for _, value := range []*float64{intervals.min, intervals.p50, intervals.p95, intervals.max} {
		if value != nil {
			return errors.New("studio report empty intervals require null quantiles")
		}
	}
	if intervals.reason == nil {
		return errors.New("studio report empty intervals require a reason")
	}
	return validateStudioP95Samples(intervals)
}

func validateStudioMeasuredIntervals(intervals studioIntervalValues) error {
	if err := validateStudioIntervalPresence(intervals); err != nil {
		return err
	}
	if err := validateStudioP95Samples(intervals); err != nil {
		return err
	}
	return validateStudioIntervalOrder(intervals)
}

func validateStudioIntervalPresence(intervals studioIntervalValues) error {
	if intervals.min == nil || intervals.p50 == nil || intervals.max == nil {
		return errors.New("studio report intervals require min, p50 and max")
	}
	if intervals.reason != nil {
		return errors.New("studio report measured intervals have no reason")
	}
	return nil
}

func validateStudioIntervalOrder(intervals studioIntervalValues) error {
	if !(*intervals.min <= *intervals.p50 && *intervals.p50 <= *intervals.max) {
		return errors.New("studio report interval quantiles are not ordered")
	}
	if intervals.p95 != nil && !(*intervals.p50 <= *intervals.p95 && *intervals.p95 <= *intervals.max) {
		return errors.New("studio report interval quantiles are not ordered")
	}
	return nil
}

func validateStudioP95Samples(intervals studioIntervalValues) error {
	if intervals.count < 20 {
		if intervals.p95 != nil || intervals.p95Reason == nil || *intervals.p95Reason != "insufficient_samples" {
			return errors.New("studio report interval p95 requires null with the insufficient-sample reason below twenty")
		}
		return nil
	}
	if intervals.p95 == nil || intervals.p95Reason != nil {
		return errors.New("studio report interval p95 is required from twenty samples")
	}
	return nil
}

func validateStudioSVGOutputs(raw json.RawMessage) error {
	items, err := studioArray(raw)
	if err != nil || len(items) < 1 {
		return errors.New("invalid studio report svg outputs")
	}
	for _, rawItem := range items {
		item, err := studioObject(rawItem)
		if err != nil {
			return err
		}
		if err := studioClosed(item, "case_id", "prompt", "status", "svg", "bytes"); err != nil {
			return err
		}
		caseID, err := studioText(item["case_id"])
		if err != nil || !studioIdentifier.MatchString(caseID) {
			return errors.New("invalid studio report svg output case id")
		}
		if _, err := studioText(item["prompt"]); err != nil {
			return err
		}
		status, err := studioText(item["status"])
		if err != nil || !slices.Contains(studioSVGStatuses, status) {
			return errors.New("invalid studio report svg output status")
		}
		svg, err := studioNullableText(item["svg"])
		if err != nil {
			return err
		}
		size, err := studioCount(item["bytes"])
		if err != nil {
			return err
		}
		if (status == "ok") != (svg != nil) {
			return errors.New("studio report svg payload must appear exactly when status is ok")
		}
		if status == "ok" && (int64(len(*svg)) != size || size > StudioMaxSVGBytes) {
			return errors.New("studio report svg size must match bounded UTF-8 byte count")
		}
		if status == "not_found" && size != 0 {
			return errors.New("studio report not_found svg output must have zero bytes")
		}
		if status == "too_large" && size <= StudioMaxSVGBytes {
			return errors.New("studio report too_large svg output must exceed limit")
		}
	}
	return nil
}
