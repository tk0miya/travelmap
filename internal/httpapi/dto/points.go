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

// Point is one location as GET /api/v1/points reports it.
//
// Latitude, Longitude, Velocity, Course and CourseAccuracy are strings, not
// numbers: that is what upstream's own serializer sends, not what its
// published schema says.
type Point struct {
	ID int64 `json:"id"`

	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	Timestamp int64  `json:"timestamp"`

	Altitude         *float64 `json:"altitude"`
	Velocity         *string  `json:"velocity"`
	Accuracy         *float64 `json:"accuracy"`
	VerticalAccuracy *float64 `json:"vertical_accuracy"`
	Course           *string  `json:"course"`
	CourseAccuracy   *string  `json:"course_accuracy"`
	BatteryStatus    *string  `json:"battery_status"`
	Battery          *float64 `json:"battery"`
	SSID             *string  `json:"ssid"`
	TrackerID        *string  `json:"tracker_id"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	UserID    int64  `json:"user_id"`

	// Below: nothing here stores any of these yet, so every point reports
	// them as the value a point that has never been reverse-geocoded,
	// imported, or assigned to a visit already carries upstream.
	Ping       *float64       `json:"ping"`
	Topic      *string        `json:"topic"`
	Trigger    *string        `json:"trigger"`
	BSSID      *string        `json:"bssid"`
	Connection *string        `json:"connection"`
	Mode       *float64       `json:"mode"`
	InRegions  []string       `json:"in_regions"`
	InRIDs     []string       `json:"inrids"`
	RawData    *string        `json:"raw_data"`
	ImportID   *string        `json:"import_id"`
	City       *string        `json:"city"`
	Country    *string        `json:"country"`
	VisitID    *string        `json:"visit_id"`
	Geodata    map[string]any `json:"geodata"`
}
