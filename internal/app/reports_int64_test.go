package app

import (
	"math"
	"strings"
	"testing"
)

func TestFinancialChartsKeepBIGINTTotalsAndScaleOnlyCoordinates(t *testing.T) {
	segments := chartSegments([]ChartSegment{
		{Label: "obligation", Value: 4_077_000_000},
		{Label: "remaining", Value: 3_077_000_000},
	})
	if segments[0].Value != 4_077_000_000 || segments[1].Value != 3_077_000_000 {
		t.Fatalf("BIGINT chart totals changed: %+v", segments)
	}
	if segments[0].Percent != 100 || segments[1].Percent < 75 || segments[1].Percent > 76 {
		t.Fatalf("unexpected overflow-safe percentages: %+v", segments)
	}

	savings := make([]int64, 12)
	loans := make([]int64, 12)
	for index := range savings {
		savings[index] = 3_000_000_000
		loans[index] = 4_077_000_000
	}
	chart := dashboardSavingsLoanComparisonChart(savings, loans)
	if len(chart.Series) != 2 || chart.Series[1].Points == "" {
		t.Fatalf("large monetary chart did not render bounded coordinates: %+v", chart)
	}
	if !strings.Contains(chart.YTicks[0].Label, "4077") {
		t.Fatalf("axis label lost original BIGINT scale: %q", chart.YTicks[0].Label)
	}

	scaled, maximum := scaleChartMoney([]int64{math.MaxInt64, -math.MaxInt64}, math.MaxInt64)
	if maximum <= 0 || scaled[0] <= 0 || scaled[1] >= 0 {
		t.Fatalf("MaxInt64 chart scaling failed: values=%v maximum=%d", scaled, maximum)
	}
}

func TestSavingsLoanComparisonChartPlotsMonthlyValues(t *testing.T) {
	savings := make([]int64, 12)
	loans := make([]int64, 12)
	for index := range savings {
		savings[index] = int64(index) * 100_000
		loans[index] = int64(11-index) * 100_000
	}

	chart := dashboardSavingsLoanComparisonChart(savings, loans)
	if len(chart.Series) != 2 || len(chart.Series[0].Dots) != 12 || len(chart.Series[1].Dots) != 12 {
		t.Fatalf("monthly chart series have wrong shape: %+v", chart.Series)
	}
	if chart.Series[0].Dots[0].Y == chart.Series[0].Dots[11].Y || chart.Series[1].Dots[0].Y == chart.Series[1].Dots[11].Y {
		t.Fatalf("monthly chart flattened a changing series: %+v", chart.Series)
	}
}

func TestRatioPercentKeepsUnboundedLossMarginsAndAvoidsOverflow(t *testing.T) {
	if got := ratioPercent(1-100, 1); got != -9900 {
		t.Fatalf("loss margin=%d want -9900", got)
	}
	if got := ratioPercent(math.MaxInt64, math.MaxInt64); got != 100 {
		t.Fatalf("large ratio=%d want 100", got)
	}
	if got := chartPercent(-99, 1); got != -100 {
		t.Fatalf("chart percentage=%d want bounded -100", got)
	}
}

func TestReportMonetaryArithmeticRejectsCrossAggregateOverflow(t *testing.T) {
	if _, err := checkedReportAdd(math.MaxInt64, 1); err == nil {
		t.Fatal("cross-table report addition overflow was silent")
	}
	if _, err := checkedReportSub(math.MinInt64, 1); err == nil {
		t.Fatal("cross-table report subtraction overflow was silent")
	}
	if got, err := checkedReportAdd(math.MaxInt64-1, 1); err != nil || got != math.MaxInt64 {
		t.Fatalf("boundary addition got=%d err=%v", got, err)
	}
}
