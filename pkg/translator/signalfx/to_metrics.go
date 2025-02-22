// Copyright 2019, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package signalfx // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/signalfx"

import (
	"fmt"

	"github.com/signalfx/com_signalfx_metrics_protobuf/model"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/multierr"
)

const numMetricTypes = 4

// ToTranslator converts from SignalFx proto data model to pdata.
type ToTranslator struct{}

// ToMetrics converts SignalFx proto data points to pmetric.Metrics.
func (tt *ToTranslator) ToMetrics(sfxDataPoints []*model.DataPoint) (pmetric.Metrics, error) {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	ilm := rm.ScopeMetrics().AppendEmpty()

	ms := ilm.Metrics()
	ms.EnsureCapacity(len(sfxDataPoints))

	// This is a map from [metric_name, metric_type] -> index + 1 in the Metrics slice. Used to combine datapoints together.
	datapointToMetric := make(map[string][numMetricTypes]int, len(sfxDataPoints))

	var err error
	for _, sfxDataPoint := range sfxDataPoints {
		if sfxDataPoint == nil {
			// TODO: Log or metric for this odd ball?
			continue
		}
		err = multierr.Append(err, setDataTypeAndPoints(sfxDataPoint, ms, datapointToMetric))
	}

	return md, err
}

func setDataTypeAndPoints(sfxDataPoint *model.DataPoint, ms pmetric.MetricSlice, datapointToMetric map[string][4]int) error {
	if sfxDataPoint.Value.IntValue == nil && sfxDataPoint.Value.DoubleValue == nil {
		return fmt.Errorf("nil datum value for data-point in metric %q", sfxDataPoint.GetMetric())
	}

	sfxMetricType := sfxDataPoint.GetMetricType()
	idxs, ok := datapointToMetric[sfxDataPoint.Metric]
	if ok && sfxMetricType < numMetricTypes && idxs[sfxMetricType] != 0 {
		m := ms.At(idxs[sfxMetricType] - 1)
		// Only emit gauge and sum.
		switch m.DataType() {
		case pmetric.MetricDataTypeGauge:
			fillNumberDataPoint(sfxDataPoint, m.Gauge().DataPoints())
		case pmetric.MetricDataTypeSum:
			fillNumberDataPoint(sfxDataPoint, m.Sum().DataPoints())
		}
		return nil
	}

	var m pmetric.Metric
	switch sfxMetricType {
	case model.MetricType_GAUGE:
		m = ms.AppendEmpty()
		// Numerical: Periodic, instantaneous measurement of some state.
		m.SetDataType(pmetric.MetricDataTypeGauge)
		fillNumberDataPoint(sfxDataPoint, m.Gauge().DataPoints())

	case model.MetricType_COUNTER:
		m = ms.AppendEmpty()
		m.SetDataType(pmetric.MetricDataTypeSum)
		m.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
		m.Sum().SetIsMonotonic(true)
		fillNumberDataPoint(sfxDataPoint, m.Sum().DataPoints())

	case model.MetricType_CUMULATIVE_COUNTER:
		m = ms.AppendEmpty()
		m.SetDataType(pmetric.MetricDataTypeSum)
		m.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
		m.Sum().SetIsMonotonic(true)
		fillNumberDataPoint(sfxDataPoint, m.Sum().DataPoints())

	default:
		return fmt.Errorf("unknown data-point type (%d) in metric %q", sfxMetricType, sfxDataPoint.Metric)
	}
	m.SetName(sfxDataPoint.Metric)

	idxs[sfxMetricType] = ms.Len()
	datapointToMetric[sfxDataPoint.Metric] = idxs
	return nil
}

func fillNumberDataPoint(sfxDataPoint *model.DataPoint, dps pmetric.NumberDataPointSlice) {
	dp := dps.AppendEmpty()
	dp.SetTimestamp(toTimestamp(sfxDataPoint.GetTimestamp()))
	switch {
	case sfxDataPoint.Value.IntValue != nil:
		dp.SetIntVal(*sfxDataPoint.Value.IntValue)
	case sfxDataPoint.Value.DoubleValue != nil:
		dp.SetDoubleVal(*sfxDataPoint.Value.DoubleValue)
	}
	fillInAttributes(sfxDataPoint.Dimensions, dp.Attributes())
}

func fillInAttributes(dimensions []*model.Dimension, attributes pcommon.Map) {
	attributes.Clear()
	attributes.EnsureCapacity(len(dimensions))

	for _, dim := range dimensions {
		if dim == nil {
			// TODO: Log or metric for this odd ball?
			continue
		}
		attributes.InsertString(dim.Key, dim.Value)
	}
}
