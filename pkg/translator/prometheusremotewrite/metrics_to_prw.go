// Copyright  The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prometheusremotewrite // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/prometheusremotewrite"

import (
	"errors"
	"fmt"

	"github.com/prometheus/prometheus/prompb"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/model/pdata"
	"go.uber.org/multierr"
)

// Deprecated: [0.45.0] use `prometheusremotewrite.FromMetrics`. It does not wrap the error as `NewPermanent`.
func MetricsToPRW(namespace string, externalLabels map[string]string, md pdata.Metrics) (map[string]*prompb.TimeSeries, int, error) {
	tsMap, err := FromMetrics(md, Settings{Namespace: namespace, ExternalLabels: externalLabels})
	if err != nil {
		err = consumererror.NewPermanent(err)
	}
	return tsMap, md.MetricCount() - len(tsMap), err
}

type Settings struct {
	Namespace      string
	ExternalLabels map[string]string
}

// FromMetrics converts pdata.Metrics to prometheus remote write format.
func FromMetrics(md pdata.Metrics, settings Settings) (tsMap map[string]*prompb.TimeSeries, errs error) {
	tsMap = make(map[string]*prompb.TimeSeries)

	resourceMetricsSlice := md.ResourceMetrics()
	for i := 0; i < resourceMetricsSlice.Len(); i++ {
		resourceMetrics := resourceMetricsSlice.At(i)
		resource := resourceMetrics.Resource()
		scopeMetricsSlice := resourceMetrics.ScopeMetrics()
		// TODO: add resource attributes as labels, probably in next PR
		for j := 0; j < scopeMetricsSlice.Len(); j++ {
			scopeMetrics := scopeMetricsSlice.At(j)
			metricSlice := scopeMetrics.Metrics()

			// TODO: decide if instrumentation library information should be exported as labels
			for k := 0; k < metricSlice.Len(); k++ {
				metric := metricSlice.At(k)

				// check for valid type and temporality combination and for matching data field and type
				if ok := validateMetrics(metric); !ok {
					errs = multierr.Append(errs, errors.New("invalid temporality and type combination"))
					continue
				}

				// handle individual metric based on type
				switch metric.DataType() {
				case pdata.MetricDataTypeGauge:
					dataPoints := metric.Gauge().DataPoints()
					if err := addNumberDataPointSlice(dataPoints, resource, metric, settings, tsMap); err != nil {
						errs = multierr.Append(errs, err)
					}
				case pdata.MetricDataTypeSum:
					dataPoints := metric.Sum().DataPoints()
					if err := addNumberDataPointSlice(dataPoints, resource, metric, settings, tsMap); err != nil {
						errs = multierr.Append(errs, err)
					}

				case pdata.MetricDataTypeHistogram:
					dataPoints := metric.Histogram().DataPoints()
					if dataPoints.Len() == 0 {
						errs = multierr.Append(errs, fmt.Errorf("empty data points. %s is dropped", metric.Name()))
					}
					for x := 0; x < dataPoints.Len(); x++ {
						addSingleHistogramDataPoint(dataPoints.At(x), resource, metric, settings, tsMap)
					}
				case pdata.MetricDataTypeSummary:
					dataPoints := metric.Summary().DataPoints()
					if dataPoints.Len() == 0 {
						errs = multierr.Append(errs, fmt.Errorf("empty data points. %s is dropped", metric.Name()))
					}
					for x := 0; x < dataPoints.Len(); x++ {
						addSingleSummaryDataPoint(dataPoints.At(x), resource, metric, settings, tsMap)
					}
				default:
					errs = multierr.Append(errs, errors.New("unsupported metric type"))
				}
			}
		}
	}

	return
}

func addNumberDataPointSlice(dataPoints pdata.NumberDataPointSlice,
	resource pdata.Resource, metric pdata.Metric,
	settings Settings, tsMap map[string]*prompb.TimeSeries) error {
	if dataPoints.Len() == 0 {
		return fmt.Errorf("empty data points. %s is dropped", metric.Name())
	}
	for x := 0; x < dataPoints.Len(); x++ {
		addSingleNumberDataPoint(dataPoints.At(x), resource, metric, settings, tsMap)
	}
	return nil
}
