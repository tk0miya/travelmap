package dto

// TrackedMonthsYear is one element of the array GET /api/v1/points/tracked_months
// answers with: the months at least one point falls on, for one year.
type TrackedMonthsYear struct {
	Year   int      `json:"year"`
	Months []string `json:"months"`
}

// Stats is the body of GET /api/v1/stats. It is camelCase throughout, unlike
// every other endpoint here — copied exactly, since a client already parses
// it that way.
type Stats struct {
	TotalDistanceKm            int64         `json:"totalDistanceKm"`
	TotalPointsTracked         int           `json:"totalPointsTracked"`
	TotalReverseGeocodedPoints int           `json:"totalReverseGeocodedPoints"`
	TotalCountriesVisited      int           `json:"totalCountriesVisited"`
	TotalCitiesVisited         int           `json:"totalCitiesVisited"`
	YearlyStats                []YearlyStats `json:"yearlyStats"`
}

// YearlyStats is one element of [Stats.YearlyStats].
type YearlyStats struct {
	Year                  int               `json:"year"`
	TotalDistanceKm       int64             `json:"totalDistanceKm"`
	TotalCountriesVisited int               `json:"totalCountriesVisited"`
	TotalCitiesVisited    int               `json:"totalCitiesVisited"`
	MonthlyDistanceKm     MonthlyDistanceKm `json:"monthlyDistanceKm"`
}

// MonthlyDistanceKm is [YearlyStats.MonthlyDistanceKm]: the year's distance
// broken down by month, keyed by the lowercase English month name rather than
// a number.
type MonthlyDistanceKm struct {
	January   int64 `json:"january"`
	February  int64 `json:"february"`
	March     int64 `json:"march"`
	April     int64 `json:"april"`
	May       int64 `json:"may"`
	June      int64 `json:"june"`
	July      int64 `json:"july"`
	August    int64 `json:"august"`
	September int64 `json:"september"`
	October   int64 `json:"october"`
	November  int64 `json:"november"`
	December  int64 `json:"december"`
}
