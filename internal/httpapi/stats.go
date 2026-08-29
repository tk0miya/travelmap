package httpapi

import (
	"net/http"
	"sort"
	"time"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/model"
)

// dailyStatsForRequest resolves the authenticated user and loads every
// daily_stats row recorded for them, writing an error response and reporting
// false if either step fails — the shared first half of [api.trackedMonths]
// and [api.stats].
func (a *api) dailyStatsForRequest(w http.ResponseWriter, r *http.Request) ([]model.DailyStat, bool) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("a daily_stats read was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return nil, false
	}

	days, err := a.store.DailyStats().All(r.Context(), user.ID)
	if err != nil {
		a.logger.Error("reading daily_stats failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return nil, false
	}

	return days, true
}

// trackedMonths answers GET /api/v1/points/tracked_months: the years and
// months the user has at least one point recorded for, read from daily_stats
// rather than aggregating points directly.
func (a *api) trackedMonths(w http.ResponseWriter, r *http.Request) {
	days, ok := a.dailyStatsForRequest(w, r)
	if !ok {
		return
	}

	a.writeJSON(w, r, http.StatusOK, trackedMonthsToDTO(days))
}

// stats answers GET /api/v1/stats: distance, point counts and country/city
// counts aggregated from daily_stats, overall and broken down by year and
// month.
func (a *api) stats(w http.ResponseWriter, r *http.Request) {
	days, ok := a.dailyStatsForRequest(w, r)
	if !ok {
		return
	}

	a.writeJSON(w, r, http.StatusOK, statsToDTO(days))
}

// sortedYearsDesc returns years' keys sorted most-recent-first — the order
// both GET /api/v1/stats and GET /api/v1/points/tracked_months group years
// in.
func sortedYearsDesc[V any](years map[int]V) []int {
	out := make([]int, 0, len(years))
	for year := range years {
		out = append(out, year)
	}

	sort.Sort(sort.Reverse(sort.IntSlice(out)))

	return out
}

// trackedMonthsToDTO converts days into the shape GET /api/v1/points/tracked_months
// answers with: years sorted most-recent-first, and within each year the
// months that have data, in calendar order. It never returns nil: an empty
// result is still a JSON array.
func trackedMonthsToDTO(days []model.DailyStat) []dto.TrackedMonthsYear {
	months := make(map[int]map[time.Month]bool)

	for _, d := range days {
		year := d.Day.Year()

		if months[year] == nil {
			months[year] = make(map[time.Month]bool)
		}

		months[year][d.Day.Month()] = true
	}

	years := sortedYearsDesc(months)

	out := make([]dto.TrackedMonthsYear, 0, len(years))

	for _, year := range years {
		monthList := make([]string, 0, 12)

		for month := time.January; month <= time.December; month++ {
			if months[year][month] {
				monthList = append(monthList, month.String()[:3])
			}
		}

		out = append(out, dto.TrackedMonthsYear{Year: year, Months: monthList})
	}

	return out
}

// yearAgg accumulates one year's worth of daily_stats rows while statsToDTO
// walks them.
type yearAgg struct {
	km        float64
	countries map[string]bool
	cities    map[string]bool
	monthKM   [12]float64 // index 0 is January.
}

// statsToDTO converts days into the shape GET /api/v1/stats answers with.
// Distances are truncated to whole kilometres rather than rounded — see
// [kmToInt].
func statsToDTO(days []model.DailyStat) dto.Stats {
	var (
		totalPoints, totalGeocoded int
		totalKM                    float64
	)

	allCountries := make(map[string]bool)
	allCities := make(map[string]bool)
	years := make(map[int]*yearAgg)

	for _, d := range days {
		totalPoints += d.Points
		totalGeocoded += d.ReverseGeocodedPoints
		totalKM += d.KM

		for _, c := range d.Countries {
			allCountries[c] = true
		}

		for _, c := range d.Cities {
			allCities[c] = true
		}

		year := d.Day.Year()

		agg, ok := years[year]
		if !ok {
			agg = &yearAgg{countries: make(map[string]bool), cities: make(map[string]bool)}
			years[year] = agg
		}

		agg.km += d.KM
		agg.monthKM[d.Day.Month()-1] += d.KM

		for _, c := range d.Countries {
			agg.countries[c] = true
		}

		for _, c := range d.Cities {
			agg.cities[c] = true
		}
	}

	yearNums := sortedYearsDesc(years)

	yearlyStats := make([]dto.YearlyStats, 0, len(yearNums))

	for _, year := range yearNums {
		agg := years[year]

		yearlyStats = append(yearlyStats, dto.YearlyStats{
			Year:                  year,
			TotalDistanceKm:       kmToInt(agg.km),
			TotalCountriesVisited: len(agg.countries),
			TotalCitiesVisited:    len(agg.cities),
			MonthlyDistanceKm: dto.MonthlyDistanceKm{
				January:   kmToInt(agg.monthKM[0]),
				February:  kmToInt(agg.monthKM[1]),
				March:     kmToInt(agg.monthKM[2]),
				April:     kmToInt(agg.monthKM[3]),
				May:       kmToInt(agg.monthKM[4]),
				June:      kmToInt(agg.monthKM[5]),
				July:      kmToInt(agg.monthKM[6]),
				August:    kmToInt(agg.monthKM[7]),
				September: kmToInt(agg.monthKM[8]),
				October:   kmToInt(agg.monthKM[9]),
				November:  kmToInt(agg.monthKM[10]),
				December:  kmToInt(agg.monthKM[11]),
			},
		})
	}

	return dto.Stats{
		TotalDistanceKm:            kmToInt(totalKM),
		TotalPointsTracked:         totalPoints,
		TotalReverseGeocodedPoints: totalGeocoded,
		TotalCountriesVisited:      len(allCountries),
		TotalCitiesVisited:         len(allCities),
		YearlyStats:                yearlyStats,
	}
}

// kmToInt truncates a fractional kilometre total to a whole number, matching
// upstream's own precision loss rather than rounding to travelmap's own
// closer approximation.
func kmToInt(km float64) int64 {
	return int64(km)
}
