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
	EnteredZones []string `json:"entered_zones"`
	// HasClip         bool    `json:"has_clip"`
	// HasSnapshot     bool    `json:"has_snapshot"`
}

const uniqueIdPrefix = "frigate_objects"

func (a *after) labelTopic(topic string) string {
	return topic + "labels/" + a.Label + "/"
}

func (a *after) labelUniqueId() string {
	return uniqueIdPrefix + "_label_" + a.Label
}

func (a *after) labelName() string {
	return a.Label
}

type discoveryPayload struct {
	DeviceClass         string `json:"device_class,omitempty"`
	Name                string `json:"name,omitempty"`
	UnitOfMeasurement   string `json:"unit_of_measurement,omitempty"`
	Icon                string `json:"icon,omitempty"`
	StateTopic          string `json:"state_topic"`
	StateClass          string `json:"state_class,omitempty"`
	UniqueID            string `json:"unique_id"`
	JsonAttributesTopic string `json:"json_attributes_topic"`
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

	payload.UniqueID = entUniqueId + "_count"
	payload.StateTopic = topic + "count"
	payload.JsonAttributesTopic = topic + "attributes"
	payload.Name = name + " count"
	if err := publish("sensor", payload); err != nil {
		return err
	}

	payload.UniqueID = entUniqueId + "_moving_count"
	payload.StateTopic = topic + "moving/count"
	payload.JsonAttributesTopic = topic + "moving/attributes"
	payload.Name = name + " moving count"
	if err := publish("sensor", payload); err != nil {
		return err
	}

	payload.UniqueID = entUniqueId + "_stationary_count"
	payload.StateTopic = topic + "stationary/count"
	payload.JsonAttributesTopic = topic + "stationary/attributes"
	payload.Name = name + " stationary count"
	if err := publish("sensor", payload); err != nil {
		return err
	}

	return nil
}

func (a *after) publishLabelDiscovery(ctx context.Context, c *paho.Client, discoveryTopic, pubTopic string) error {
	if discoveryTopic != "" {
		return publishDiscovery(ctx, c, discoveryTopic, pubTopic, a.labelUniqueId(), a.labelTopic(pubTopic), a.labelName())
	}

	return nil
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

		if _, ok := discoveryEntities[afterMsg.labelUniqueId()]; !ok {
			if err := afterMsg.publishLabelDiscovery(ctx, c, discoveryTopic, pubTopic); err != nil {
				return err
			}
			discoveryEntities[afterMsg.labelUniqueId()] = struct{}{}
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

type updates struct {
	total *counts
	label *counts
}

func newUpdates() *updates {
	return &updates{
		total: newCounts(),
		label: newCounts(),
	}
}

type counts struct {
	count      *attributes
	moving     *attributes
	stationary *attributes
}

func newCounts() *counts {
	return &counts{
		count:      newAttributes(),
		moving:     newAttributes(),
		stationary: newAttributes(),
	}
}

func (oc *counts) publish(ctx context.Context, c *paho.Client, topic string) error {
	if err := oc.count.publish(ctx, c, topic); err != nil {
		return err
	}

	if err := oc.moving.publish(ctx, c, topic+"moving/"); err != nil {
		return err
	}

	if err := oc.stationary.publish(ctx, c, topic+"stationary/"); err != nil {
		return err
	}

	return nil
}

func (u *updates) publish(ctx context.Context, c *paho.Client, afterMsg *after, pubTopic string) error {
	if err := u.total.publish(ctx, c, pubTopic+"object/"); err != nil {
		return err
	}

	if err := u.label.publish(ctx, c, afterMsg.labelTopic(pubTopic)); err != nil {
		return err
	}

	return nil
}

type attributes struct {
	Count        int            `json:"count"`
	Cameras      map[string]int `json:"cameras"`
	EnteredZones map[string]int `json:"entered_zones"`
	CurrentZones map[string]int `json:"current_zones"`
}

func newAttributes() *attributes {
	return &attributes{
		Count:        0,
		Cameras:      make(map[string]int),
		EnteredZones: make(map[string]int),
		CurrentZones: make(map[string]int),
	}
}

func incrementMap(m map[string]int, key string) {
	if _, ok := m[key]; !ok {
		m[key] = 0
	}
	m[key] += 1
}

func (a *attributes) publish(ctx context.Context, c *paho.Client, topic string) error {
	if err := publishJson(ctx, c, topic+"attributes", a); err != nil {
		return err
	}

	if err := publishInt(ctx, c, topic+"count", a.Count); err != nil {
		return err
	}

	return nil
}

func (a *attributes) summarize(event *after) {
	a.Count += 1
	incrementMap(a.Cameras, event.Camera)

	for _, z := range event.EnteredZones {
		incrementMap(a.EnteredZones, z)
	}

	for _, z := range event.CurrentZones {
		incrementMap(a.CurrentZones, z)
	}
}

func (a *after) getCounts(events map[string]*after) *updates {
	u := newUpdates()
	for _, e := range events {
		moving := !e.Stationary
		isLabel := e.Label == a.Label

		u.total.count.summarize(e)

		if moving {
			u.total.moving.summarize(e)
		} else {
			u.total.stationary.summarize(e)
		}

		if isLabel {
			u.label.count.summarize(e)
			if moving {
				u.label.moving.summarize(e)
			} else {
				u.label.stationary.summarize(e)
			}
		}
	}

	return u
}
