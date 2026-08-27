package dto

// LocationsRequest is the body of POST /api/v1/points and
// POST /api/v1/overland/batches: both take the same GeoJSON shape, and differ
// only in the status code of a successful response.
type LocationsRequest struct {
	Locations []Feature `json:"locations"`
}

// Feature is one GeoJSON Feature carrying a point.
type Feature struct {
	Type       string          `json:"type"`
	Geometry   Geometry        `json:"geometry"`
	Properties PointProperties `json:"properties"`
}

// Geometry is the `geometry` object of a [Feature]: a GeoJSON Point, as
// [longitude, latitude].
type Geometry struct {
	Type        string    `json:"type"`
	Coordinates []float64 `json:"coordinates"`
}

// PointProperties is the `properties` object of a [Feature].
//
// Every field but Timestamp is a pointer, so that a property the device did
// not send round-trips as "not reported" rather than as the zero value of its
// type — a speed of exactly 0 and a speed the device never sent are not the
// same point.
type PointProperties struct {
	Timestamp string `json:"timestamp"`

	HorizontalAccuracy *float64 `json:"horizontal_accuracy"`
	VerticalAccuracy   *float64 `json:"vertical_accuracy"`
	Altitude           *float64 `json:"altitude"`
	Speed              *float64 `json:"speed"`
	SpeedAccuracy      *float64 `json:"speed_accuracy"`
	Course             *float64 `json:"course"`
	CourseAccuracy     *float64 `json:"course_accuracy"`
	BatteryState       *string  `json:"battery_state"`
	BatteryLevel       *float64 `json:"battery_level"`
	Wifi               *string  `json:"wifi"`
	TrackID            *string  `json:"track_id"`
	DeviceID           *string  `json:"device_id"`
}

// LocationsCreated is the body of a successful POST /api/v1/points or
// POST /api/v1/overland/batches. It is not upstream's own response body.
type LocationsCreated struct {
	Created int `json:"created"`
}
