package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/eclipse/paho.golang/paho"
)

type UnixTime struct {
	time.Time
}

func (u *UnixTime) UnmarshalJSON(b []byte) error {
	var timeFloat float64
	if err := json.Unmarshal(b, &timeFloat); err != nil {
		return err
	}

	sec, dec := math.Modf(timeFloat)
	u.Time = time.Unix(int64(sec), int64(dec*(1e9)))

	return nil
}

type frigateMessage struct {
	// Before struct {
	// 	ID              string  `json:"id"`
	// 	Camera          string  `json:"camera"`
	// 	//FrameTime       float64 `json:"frame_time"`
	// 	//SnapshotTime    float64 `json:"snapshot_time"`
	// 	Label           string  `json:"label"`
	// 	SubLabel        any     `json:"sub_label"`
	// 	TopScore        float64 `json:"top_score"`
	// 	FalsePositive   bool    `json:"false_positive"`
	// 	StartTime       float64 `json:"start_time"`
	// 	EndTime         any     `json:"end_time"`
	// 	Score           float64 `json:"score"`
	// 	Box             []int   `json:"box"`
	// 	Area            int     `json:"area"`
	// 	Ratio           float64 `json:"ratio"`
	// 	Region          []int   `json:"region"`
	// 	Stationary      bool    `json:"stationary"`
	// 	MotionlessCount int     `json:"motionless_count"`
	// 	PositionChanges int     `json:"position_changes"`
	// 	CurrentZones    []any   `json:"current_zones"`
	// 	EnteredZones    []any   `json:"entered_zones"`
	// 	HasClip         bool    `json:"has_clip"`
	// 	HasSnapshot     bool    `json:"has_snapshot"`
	// } `json:"before"`
	After after  `json:"after"`
	Type  string `json:"type"`
}

type after struct {
	ID        string   `json:"id"`
	Camera    string   `json:"camera"`
	FrameTime UnixTime `json:"frame_time"`
	// SnapshotTime    float64 `json:"snapshot_time"`
	Label    string `json:"label"`
	SubLabel string `json:"sub_label"`
	// TopScore        float64 `json:"top_score"`
	// FalsePositive   bool    `json:"false_positive"`
	StartTime UnixTime `json:"start_time"`
	EndTime   UnixTime `json:"end_time"`
	// Score           float64 `json:"score"`
	// Box             []int   `json:"box"`
	// Area            int     `json:"area"`
	// Ratio           float64 `json:"ratio"`
	// Region          []int   `json:"region"`
	Stationary bool `json:"stationary"`
	// MotionlessCount int     `json:"motionless_count"`
	// PositionChanges int     `json:"position_changes"`
	CurrentZones []string `json:"current_zones"`
	// EnteredZones    []any   `json:"entered_zones"`
	// HasClip         bool    `json:"has_clip"`
	// HasSnapshot     bool    `json:"has_snapshot"`
}

const uniqueIdPrefix = "frigate_objects"

func (a *after) cameraTopic(topic string) string {
	return topic + "cameras/" + a.Camera + "/object/"
}

func (a *after) cameraUniqueId() string {
	return uniqueIdPrefix + "_camera_" + a.Camera + "_object"
}

func (a *after) cameraName() string {
	return a.Camera + " objects"
}

func (a *after) labelTopic(topic string) string {
	return topic + "labels/" + a.Label + "/"
}

func (a *after) labelUniqueId() string {
	return uniqueIdPrefix + "_label_" + a.Label
}

func (a *after) labelName() string {
	return a.Label
}

func (a *after) cameraLabelTopic(topic string) string {
	return topic + "cameras/" + a.Camera + "/labels/" + a.Label + "/"
}

func (a *after) cameraLabelUniqueId() string {
	return uniqueIdPrefix + "_camera_" + a.Camera + "_label_" + a.Label
}

func (a *after) cameraLabelName() string {
	return a.Camera + " " + a.Label
}

type discoveryPayload struct {
	DeviceClass       string `json:"device_class,omitempty"`
	Name              string `json:"name,omitempty"`
	UnitOfMeasurement string `json:"unit_of_measurement,omitempty"`
	Icon              string `json:"icon,omitempty"`
	StateTopic        string `json:"state_topic"`
	StateClass        string `json:"state_class,omitempty"`
	UniqueID          string `json:"unique_id"`
	// TODO: Match on frigate device
	// For now we will just use one device id for this
	Device struct {
		Identifiers  []string `json:"identifiers,omitempty"`
		Name         string   `json:"name,omitempty"`
		Model        string   `json:"model,omitempty"`
		Manufacturer string   `json:"manufacturer,omitempty"`
	} `json:"device,omitempty"`
}

const (
	deviceName     = "Frigate Objects"
	occupancyClass = "occupancy"
)

func publishDiscovery(
	ctx context.Context,
	c *paho.Client,
	discoveryTopic,
	pubTopic,
	entUniqueId,
	topic,
	name string,
) error {
	publish := func(component string, payload *discoveryPayload) error {
		return publishJson(ctx, c, discoveryTopic+"/"+component+"/"+uniqueIdPrefix+"/"+payload.UniqueID+"/config", payload)
	}

	payload := &discoveryPayload{}
	payload.StateClass = "measurement"
	payload.Device.Identifiers = []string{pubTopic}
	payload.Device.Name = deviceName

	//TODO Need motion or stationary

	payload.UniqueID = entUniqueId + "_count"
	payload.StateTopic = topic + "count"
	payload.Name = name + " count"
	if err := publish("sensor", payload); err != nil {
		return err
	}

	payload.UniqueID = entUniqueId + "_moving_count"
	payload.StateTopic = topic + "moving/count"
	payload.Name = name + " moving count"
	if err := publish("sensor", payload); err != nil {
		return err
	}

	payload.UniqueID = entUniqueId + "_stationary_count"
	payload.StateTopic = topic + "stationary/count"
	payload.Name = name + " stationary count"
	if err := publish("sensor", payload); err != nil {
		return err
	}

	payload.DeviceClass = occupancyClass
	payload.StateClass = ""

	payload.UniqueID = entUniqueId + "_detected"
	payload.StateTopic = topic + "detected"
	payload.Name = name + " detected"
	if err := publish("binary_sensor", payload); err != nil {
		return err
	}

	payload.UniqueID = entUniqueId + "_moving_detected"
	payload.StateTopic = topic + "moving/detected"
	payload.Name = name + " moving detected"
	if err := publish("binary_sensor", payload); err != nil {
		return err
	}

	payload.UniqueID = entUniqueId + "_stationary_detected"
	payload.StateTopic = topic + "stationary/detected"
	payload.Name = name + " stationary detected"
	if err := publish("binary_sensor", payload); err != nil {
		return err
	}

	return nil
}

func (a *after) publishCameraDiscovery(ctx context.Context, c *paho.Client, discoveryTopic, pubTopic string) error {
	return publishDiscovery(ctx, c, discoveryTopic, pubTopic, a.cameraUniqueId(), a.cameraTopic(pubTopic), a.cameraName())
}

func (a *after) publishLabelDiscovery(ctx context.Context, c *paho.Client, discoveryTopic, pubTopic string) error {
	return publishDiscovery(ctx, c, discoveryTopic, pubTopic, a.labelUniqueId(), a.labelTopic(pubTopic), a.labelName())
}

func (a *after) publishCameraLabelDiscovery(
	ctx context.Context,
	c *paho.Client,
	discoveryTopic,
	pubTopic string,
) error {
	return publishDiscovery(ctx, c, discoveryTopic, pubTopic, a.cameraLabelUniqueId(), a.cameraLabelTopic(pubTopic),
		a.cameraLabelName())
}

func publishObjectDiscovery(ctx context.Context, c *paho.Client, discoveryTopic, pubTopic string) error {
	return publishDiscovery(ctx, c, discoveryTopic, pubTopic, uniqueIdPrefix+"_objects", pubTopic+"objects/", "Objects")
}

const MqttKeepAlive = 30

var logger *log.Logger = log.New(os.Stdout, "SUB: ", log.LstdFlags)

func main() {
	server := flag.String("server", "127.0.0.1:1883", "The MQTT server to connect to ex: 127.0.0.1:1883")
	subTopic := flag.String("subscribe-topic", "frigate/events", "Topic to subscribe to")
	pubTopic := flag.String("publish-topic", "frigate_objects", "Topic to publish to")
	discoveryTopic := flag.String("discovery-topic", "homeassistant",
		"home assistant discovery topic (set to empty to disable discovery)")
	qos := flag.Int("qos", 0, "The QoS to subscribe to messages at")
	clientid := flag.String("clientid", "frigate_hass_object_events", "A clientid for the connection")
	username := flag.String("username", "", "A username to authenticate to the MQTT server")
	password := flag.String("password", "", "Password to match username")
	flag.Parse()

	cp := &paho.Connect{
		KeepAlive:  MqttKeepAlive,
		ClientID:   *clientid,
		CleanStart: true,
		Username:   *username,
		Password:   []byte(*password),
	}

	if *username != "" {
		cp.UsernameFlag = true
	}

	if *password != "" {
		cp.PasswordFlag = true
	}

	ctx, cancel := context.WithCancel(context.Background())

	fmt.Printf("Connected to %s\n", *server)

	ic := make(chan os.Signal, 1)
	signal.Notify(ic, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-ic
		fmt.Println("signal received, exiting")
		cancel()
	}()

	err := subscribe(ctx, *server, cp, *subTopic, *pubTopic, *discoveryTopic, *qos)
	if err != nil {
		log.Fatalln(err)
	}
}

func subscribe(
	ctx context.Context,
	server string,
	cp *paho.Connect,
	subTopic string,
	pubTopic string,
	discoveryTopic string,
	qos int,
) error {
	conn, err := net.Dial("tcp", server)
	if err != nil {
		return err
	}

	msgChan := make(chan *paho.Publish)

	c := paho.NewClient(paho.ClientConfig{
		Router: paho.NewStandardRouterWithDefault(func(m *paho.Publish) {
			msgChan <- m
		}),
		Conn: conn,
	})

	ca, err := c.Connect(ctx, cp)
	if err != nil {
		return err
	}

	if ca.ReasonCode != 0 {
		return fmt.Errorf("Failed to connect to %s : %d - %s", server, ca.ReasonCode, ca.Properties.ReasonString)
	}

	context.AfterFunc(ctx, func() {
		if c != nil {
			d := &paho.Disconnect{ReasonCode: 0}
			c.Disconnect(d)
		}
		close(msgChan)
	})

	// c.SetDebugLogger(logger)
	c.SetErrorLogger(logger)
	sa, err := c.Subscribe(context.Background(), &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: subTopic, QoS: byte(qos)},
		},
	})
	if err != nil {
		return err
	}

	if sa.Reasons[0] != byte(qos) {
		return fmt.Errorf("Failed to subscribe to %s : %d", subTopic, sa.Reasons[0])
	}

	log.Printf("Subscribed to %s", subTopic)

	return processEvents(ctx, c, msgChan, pubTopic, discoveryTopic)
}

func processEvents(
	ctx context.Context,
	c *paho.Client,
	msgChan <-chan *paho.Publish,
	pubTopic string,
	discoveryTopic string,
) error {
	pubTopic = pubTopic + "/"
	events := make(map[string]*after)
	discoveryEntities := make(map[string]struct{})

	if err := publishObjectDiscovery(ctx, c, discoveryTopic, pubTopic); err != nil {
		return err
	}

	for m := range msgChan {
		var fm frigateMessage
		if err := json.Unmarshal(m.Payload, &fm); err != nil {
			return fmt.Errorf("Failed to unmarshal message: %w", err)
		}

		afterMsg := fm.After

		if fm.After.EndTime.Unix() != 0 {
			delete(events, afterMsg.ID)
		} else {
			events[afterMsg.ID] = &fm.After
		}

		os := afterMsg.getCounts(events)
		os.publish(ctx, c, &afterMsg, pubTopic)

		if _, ok := discoveryEntities[afterMsg.cameraUniqueId()]; !ok {
			if err := afterMsg.publishCameraDiscovery(ctx, c, discoveryTopic, pubTopic); err != nil {
				return err
			}
			discoveryEntities[afterMsg.cameraUniqueId()] = struct{}{}
		}

		if _, ok := discoveryEntities[afterMsg.labelUniqueId()]; !ok {
			if err := afterMsg.publishLabelDiscovery(ctx, c, discoveryTopic, pubTopic); err != nil {
				return err
			}
			discoveryEntities[afterMsg.labelUniqueId()] = struct{}{}
		}

		if _, ok := discoveryEntities[afterMsg.cameraLabelUniqueId()]; !ok {
			if err := afterMsg.publishCameraLabelDiscovery(ctx, c, discoveryTopic, pubTopic); err != nil {
				return err
			}
			discoveryEntities[afterMsg.cameraLabelUniqueId()] = struct{}{}
		}
	}

	return nil
}

func publishInt(ctx context.Context, c *paho.Client, topic string, val int) error {
	_, err := c.Publish(ctx, &paho.Publish{
		Topic:   topic,
		Payload: []byte(strconv.Itoa(val)),
		Retain:  true,
	})

	return err
}

func publishBool(ctx context.Context, c *paho.Client, topic string, val bool) error {
	v := "OFF"
	if val {
		v = "ON"
	}

	_, err := c.Publish(ctx, &paho.Publish{
		Topic:   topic,
		Payload: []byte(v),
		Retain:  true,
	})

	return err
}

func publishJson(ctx context.Context, c *paho.Client, topic string, val any) error {
	j, err := json.Marshal(val)
	if err != nil {
		return err
	}

	_, err = c.Publish(ctx, &paho.Publish{
		Topic:   topic,
		Payload: j,
		Retain:  true,
	})

	return err
}

type objectSummary struct {
	total       objectCount
	camera      objectCount
	label       objectCount
	cameraLabel objectCount
}

type objectCount struct {
	count           int
	movingCount     int
	stationaryCount int
}

func (oc *objectCount) publish(ctx context.Context, c *paho.Client, topic string) error {
	if err := publishInt(ctx, c, topic+"count", oc.count); err != nil {
		return err
	}

	if err := publishBool(ctx, c, topic+"detected", oc.count > 0); err != nil {
		return err
	}

	if err := publishInt(ctx, c, topic+"moving/count", oc.movingCount); err != nil {
		return err
	}

	if err := publishBool(ctx, c, topic+"moving/detected", oc.movingCount > 0); err != nil {
		return err
	}

	if err := publishInt(ctx, c, topic+"stationary/count", oc.stationaryCount); err != nil {
		return err
	}

	if err := publishBool(ctx, c, topic+"stationary/detected", oc.stationaryCount > 0); err != nil {
		return err
	}

	return nil
}

func (os *objectSummary) publish(ctx context.Context, c *paho.Client, afterMsg *after, pubTopic string) error {
	if err := os.total.publish(ctx, c, pubTopic+"object/"); err != nil {
		return err
	}

	if err := os.camera.publish(ctx, c, afterMsg.cameraTopic(pubTopic)); err != nil {
		return err
	}

	if err := os.label.publish(ctx, c, afterMsg.labelTopic(pubTopic)); err != nil {
		return err
	}

	if err := os.cameraLabel.publish(ctx, c, afterMsg.cameraLabelTopic(pubTopic)); err != nil {
		return err
	}

	return nil
}

func (a *after) getCounts(events map[string]*after) *objectSummary {
	oc := &objectSummary{}
	for _, event := range events {
		moving := !event.Stationary
		isLabel := event.Label == a.Label
		isCamera := event.Camera == a.Camera

		oc.total.count += 1

		if moving {
			oc.total.movingCount += 1
		} else {
			oc.total.stationaryCount += 1
		}

		if isLabel {
			oc.label.count += 1
			if moving {
				oc.label.movingCount += 1
			} else {
				oc.label.stationaryCount += 1
			}
		}

		if isCamera {
			oc.camera.count += 1
			if moving {
				oc.camera.movingCount += 1
			} else {
				oc.camera.stationaryCount += 1
			}
		}

		if isCamera && isLabel {
			oc.cameraLabel.count += 1
			if moving {
				oc.cameraLabel.movingCount += 1
			} else {
				oc.cameraLabel.stationaryCount += 1
			}
		}
	}

	return oc
}
