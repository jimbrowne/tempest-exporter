package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/gorilla/handlers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// apiURL is the base API URL for the weatherflow observations API
const apiURL = "https://swd.weatherflow.com/swd/rest/observations/station"

// ns is the metric namespace prefix
const ns = "tempest"

// ss is the metric subsystem prefix
const ss = "station"

var (
	// token is our weatherflow API token
	token = os.Getenv("WEATHERFLOW_API_TOKEN")
	// station is the station ID we want to query
	station = os.Getenv("WEATHERFLOW_STATION_ID")
	// listenPort is the TCP port on which to listen for HTTP requests
	listenPort = os.Getenv("WEATHERFLOW_LISTEN_PORT")
	// labels is a map of prometheus labels to apply to the metrics retrieved
	labels     prometheus.Labels
	labelNames []string
	// metrics is an empty MetricsMap
	metrics = make(MetricsMap)
)

type logWriter struct{}

func (l *logWriter) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().Format("02/Jan/2006:15:04:05 -0700") + " [INFO] [exporter]" + string(bytes))
}

// stationStatus holds our station status code
type stationStatus struct {
	Code int `json:"status_code"`
}

// observation is the typed observation data from a station
type observation struct {
	AirDensity                        float64 `json:"air_density"`
	AirDensityIndoor                  float64 `json:"air_density_indoor"`
	AirTemperature                    float64 `json:"air_temperature"`
	AirTemperatureIndoor              float64 `json:"air_temperature_indoor"`
	BarometricPressure                float64 `json:"barometric_pressure"`
	BarometricPressureIndoor          float64 `json:"barometric_pressure_indoor"`
	Brightness                        float64 `json:"brightness"`
	DeltaT                            float64 `json:"delta_t"`
	DeltaTIndoor                      float64 `json:"delta_t_indoor"`
	DewPoint                          float64 `json:"dew_point"`
	DewPointIndoor                    float64 `json:"dew_point_indoor"`
	FeelsLike                         float64 `json:"feels_like"`
	FeelsLikeIndoor                   float64 `json:"feels_like_indoor"`
	HeatIndex                         float64 `json:"heat_index"`
	HeatIndexIndoor                   float64 `json:"heat_index_indoor"`
	LightningStrikeCount              float64 `json:"lightning_strike_count"`
	LightningStrikeCountIndoor        float64 `json:"lightning_strike_count_indoor"`
	LightningStrikeCountLast1hr       float64 `json:"lightning_strike_count_last_1hr"`
	LightningStrikeCountLast1hrIndoor float64 `json:"lightning_strike_count_last_1hr_indoor"`
	LightningStrikeCountLast3hr       float64 `json:"lightning_strike_count_last_3hr"`
	LightningStrikeCountLast3hrIndoor float64 `json:"lightning_strike_count_last_3hr_indoor"`
	LightningStrikeLastDistance       float64 `json:"lightning_strike_last_distance"`
	LightningStrikeLastDistanceIndoor float64 `json:"lightning_strike_last_distance_indoor"`
	LightningStrikeLastEpoch          float64 `json:"lightning_strike_last_epoch"`
	LightningStrikeLastEpochIndoor    float64 `json:"lightning_strike_last_epoch_indoor"`
	Precip                            float64 `json:"precip"`
	PrecipAccumLast1hr                float64 `json:"precip_accum_last_1hr"`
	PrecipAccumLocalDay               float64 `json:"precip_accum_local_day"`
	PrecipAccumLocalYesterday         float64 `json:"precip_accum_local_yesterday"`
	PrecipAccumLocalYesterdayFinal    float64 `json:"precip_accum_local_yesterday_final"`
	PrecipAnalysisTypeYesterday       float64 `json:"precip_analysis_type_yesterday"`
	PrecipMinutesLocalDay             float64 `json:"precip_minutes_local_day"`
	PrecipMinutesLocalYesterday       float64 `json:"precip_minutes_local_yesterday"`
	PrecipMinutesLocalYesterdayFinal  float64 `json:"precip_minutes_local_yesterday_final"`
	PressureTrend                     string  `json:"pressure_trend"`
	PressureTrendIndoor               string  `json:"pressure_trend_indoor"`
	RelativeHumidity                  float64 `json:"relative_humidity"`
	RelativeHumidityIndoor            float64 `json:"relative_humidity_indoor"`
	SeaLevelPressure                  float64 `json:"sea_level_pressure"`
	SeaLevelPressureIndoor            float64 `json:"sea_level_pressure_indoor"`
	SolarRadiation                    float64 `json:"solar_radiation"`
	StationPressure                   float64 `json:"station_pressure"`
	StationPressureIndoor             float64 `json:"station_pressure_indoor"`
	Timestamp                         float64 `json:"timestamp"`
	Uv                                float64 `json:"uv"`
	WetBulbTemperature                float64 `json:"wet_bulb_temperature"`
	WetBulbTemperatureIndoor          float64 `json:"wet_bulb_temperature_indoor"`
	WindAvg                           float64 `json:"wind_avg"`
	WindChill                         float64 `json:"wind_chill"`
	WindChillIndoor                   float64 `json:"wind_chill_indoor"`
	WindDirection                     float64 `json:"wind_direction"`
	WindGust                          float64 `json:"wind_gust"`
	WindLull                          float64 `json:"wind_lull"`
}

// response is our response from the weatherflow obvservations API
type response struct {
	StationId   int           `json:"station_id"`
	StationName string        `json:"station_name"`
	PublicName  string        `json:"public_name"`
	Latitude    float64       `json:"latitude"`
	Longitude   float64       `json:"longitude"`
	Timezone    string        `json:"timezone"`
	Elevation   float64       `json:"elevation"`
	Status      stationStatus `json:"status"`
	Obs         []observation `json:"obs"`
}

func mapIndoor(obs *observation) {
	v := reflect.ValueOf(obs)
	typeOfObs := v.Elem().Type()

	for i := 0; i < v.Elem().NumField(); i++ {
		fieldName := typeOfObs.Field(i).Name
		// fmt.Printf("Checking field %s\n", fieldName)
		indoorName := fieldName + "Indoor"
		indoorValue := v.Elem().FieldByName(indoorName)
		if indoorValue.IsValid() && !indoorValue.IsZero() {
			// fmt.Printf("Field: %s has an Indoor and its value is %v\n", fieldName, indoorValue)
			v.Elem().FieldByName(fieldName).Set(indoorValue)
		}
	}
}

// getTempestData retrieves the API response from our Tempest weather station
func getTempestData(t, s string) (response, error) {
	var r response
	reqURL := apiURL + "/" + s + "?token=" + t
	httpResp, err := http.Get(reqURL)
	// TODO handle client errors
	if err != nil {
		return r, fmt.Errorf("error getting data from tempest station: %v", err)
	}
	defer httpResp.Body.Close()
	err = json.NewDecoder(httpResp.Body).Decode(&r)
	if err != nil {
		return r, fmt.Errorf("error parsing json into response struct: %v", err)
	}
	return r, nil
}

// parseLabels returns a list of label values as strings matchingour "labels" var
func (r *response) parseLabels() prometheus.Labels {
	l := make(map[string]string)
	l["station_id"] = strconv.Itoa(r.StationId)
	l["station_name"] = r.StationName
	l["public_name"] = r.PublicName
	l["latitude"] = strconv.FormatFloat(r.Latitude, 'E', -1, 64)
	l["longitude"] = strconv.FormatFloat(r.Longitude, 'E', -1, 64)
	l["timezone"] = r.Timezone
	l["elevation"] = strconv.FormatFloat(r.Elevation, 'E', -1, 64)
	return l
}

// getDatas gets all the datas
func getDatas() {
	for {
		log.Println("getting latest observation...")
		r, err := getTempestData(token, station)
		if err != nil {
			log.Fatal(err)
		}
		labels = r.parseLabels()
		if len(r.Obs) > 0 {
			o := r.Obs[0]
			mapIndoor(&o)
			metrics.SetAll(o, labels)
		}
		time.Sleep(time.Second * 15)
	}
}

func init() {
	// Setup logger for non req logs
	log.SetFlags(0)
	log.SetOutput(new(logWriter))

	// Check config values
	if token == "" {
		log.Fatalln("please set WEATHERFLOW_API_TOKEN")
	}
	if station == "" {
		log.Fatalln("please set WEATHERFLOW_STATION_ID")
	}
	if listenPort == "" {
		listenPort = "6969"
	}
	// Initialize labels
	r, err := getTempestData(token, station)
	if err != nil {
		log.Fatal(err)
	}
	labels = r.parseLabels()
	labelNames = []string{}
	for k := range labels {
		labelNames = append(labelNames, k)
	}
	// Initialze metrics
	metrics.Register(labelNames)
}

func main() {
	go getDatas()

	http.Handle("/metrics", handlers.LoggingHandler(os.Stdout, promhttp.Handler()))
	http.ListenAndServe(fmt.Sprintf("0.0.0.0:%s", listenPort), nil)
}
