package httpapi

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/model"
)

// day builds the UTC midnight [time.Time] a daily_stats row's Day holds, for
// a test's fixture to read year and month off of.
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// TestStatsToDTO exercises the aggregation httpapi_test's golden-file tests
// cannot: they never populate Countries/Cities, since nothing writes them
// until reverse geocoding lands, and they exercise only one non-zero month
// per year, which cannot distinguish truncating the summed float from
// summing already-truncated per-day or per-month values.
func TestStatsToDTO(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		days []model.DailyStat
		want dto.Stats
	}{
		"no data": {
			days: nil,
			want: dto.Stats{YearlyStats: []dto.YearlyStats{}},
		},
		"truncates the summed float rather than each day's own value": {
			// Two days at 0.6 km each sum to 1.2, which truncates to 1 — a
			// bug that truncated per day first would sum 0 + 0 = 0 instead.
			days: []model.DailyStat{
				{Day: day(2024, time.January, 1), Points: 1, KM: 0.6},
				{Day: day(2024, time.January, 2), Points: 1, KM: 0.6},
			},
			want: dto.Stats{
				TotalDistanceKm:    1,
				TotalPointsTracked: 2,
				YearlyStats: []dto.YearlyStats{
					{
						Year:            2024,
						TotalDistanceKm: 1,
						MonthlyDistanceKm: dto.MonthlyDistanceKm{
							January: 1,
						},
					},
				},
			},
		},
		"dedupes countries and cities across days, per year and overall": {
			days: []model.DailyStat{
				{Day: day(2023, time.January, 1), Points: 1, Countries: []string{"Japan", "USA"}, Cities: []string{"Tokyo"}},
				{Day: day(2023, time.February, 1), Points: 1, Countries: []string{"Japan"}, Cities: []string{"Osaka"}},
				{Day: day(2024, time.March, 1), Points: 1, Countries: []string{"France"}, Cities: []string{"Paris"}},
			},
			want: dto.Stats{
				TotalPointsTracked:    3,
				TotalCountriesVisited: 3,
				TotalCitiesVisited:    3,
				YearlyStats: []dto.YearlyStats{
					{
						Year:                  2024,
						TotalCountriesVisited: 1,
						TotalCitiesVisited:    1,
					},
					{
						Year:                  2023,
						TotalCountriesVisited: 2,
						TotalCitiesVisited:    2,
					},
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, statsToDTO(tt.days)); diff != "" {
				t.Errorf("statsToDTO() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSortedYearsDesc pins the ordering helper both statsToDTO and
// trackedMonthsToDTO share.
func TestSortedYearsDesc(t *testing.T) {
	t.Parallel()

	got := sortedYearsDesc(map[int]bool{2021: true, 2024: true, 2023: true})

	if diff := cmp.Diff([]int{2024, 2023, 2021}, got); diff != "" {
		t.Errorf("sortedYearsDesc() mismatch (-want +got):\n%s", diff)
	}
}
