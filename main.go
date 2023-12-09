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

type FrigateMessage struct {
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
	After After  `json:"after"`
	Type  string `json:"type"`
}

type After struct {
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

const MqttKeepAlive = 30

var logger *log.Logger = log.New(os.Stdout, "SUB: ", log.LstdFlags)

func main() {
	server := flag.String("server", "127.0.0.1:1883", "The MQTT server to connect to ex: 127.0.0.1:1883")
	subTopic := flag.String("subscribe-topic", "frigate/events", "Topic to subscribe to")
	pubTopic := flag.String("publish-topic", "frigate_objects", "Topic to publish to")
	enableDiscovery := flag.Bool("enable-discovery", true, "Enable home assistant discovery")
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

	err := subscribe(ctx, *server, cp, *subTopic, *pubTopic, *enableDiscovery, *qos)
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
	enableDiscovery bool,
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

	return processEvents(ctx, c, msgChan, pubTopic)
}

func processEvents(ctx context.Context, c *paho.Client, msgChan <-chan *paho.Publish, pubTopic string) error {
	pubTopic = pubTopic + "/"
	events := make(map[string]*After)

	for m := range msgChan {
		var fm FrigateMessage
		if err := json.Unmarshal(m.Payload, &fm); err != nil {
			return fmt.Errorf("Failed to unmarshal message: %w", err)
		}

		after := fm.After

		if fm.After.EndTime.Unix() != 0 {
			log.Printf("Deleting ID: %s", fm.After.ID)
			delete(events, after.ID)
		} else {
			events[after.ID] = &fm.After
		}

		go func() {
			os := getCounts(&after, events)
			os.publish(ctx, c, &after, pubTopic)
		}()
	}

	return nil
}

func publishVal(ctx context.Context, c *paho.Client, topic string, val int) error {
	_, err := c.Publish(ctx, &paho.Publish{
		Topic:   topic,
		Payload: []byte(strconv.Itoa(val)),
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
	total      int
	moving     int
	stationary int
}

func (oc *objectCount) publish(ctx context.Context, c *paho.Client, topic string) error {
	if err := publishVal(ctx, c, topic+"total", oc.total); err != nil {
		return err
	}

	if err := publishVal(ctx, c, topic+"moving", oc.moving); err != nil {
		return err
	}

	if err := publishVal(ctx, c, topic+"stationary", oc.stationary); err != nil {
		return err
	}

	return nil
}

func (os *objectSummary) publish(ctx context.Context, c *paho.Client, after *After, pubTopic string) error {
	if err := os.total.publish(ctx, c, pubTopic+"objects/"); err != nil {
		return err
	}

	if err := os.camera.publish(ctx, c, pubTopic+"cameras/"+after.Camera+"/objects/"); err != nil {
		return err
	}

	if err := os.label.publish(ctx, c, pubTopic+"labels/"+after.Label+"/"); err != nil {
		return err
	}

	if err := os.cameraLabel.publish(ctx, c, pubTopic+"cameras/"+after.Camera+"/labels/"+after.Label+"/"); err != nil {
		return err
	}

	return nil
}

func getCounts(after *After, events map[string]*After) *objectSummary {
	oc := &objectSummary{}
	for _, event := range events {
		moving := !event.Stationary
		isLabel := event.Label == after.Label
		isCamera := event.Camera == after.Camera

		oc.total.total += 1

		if moving {
			oc.total.moving += 1
		} else {
			oc.total.stationary += 1
		}

		if isLabel {
			oc.label.total += 1
			if moving {
				oc.label.moving += 1
			} else {
				oc.label.stationary += 1
			}
		}

		if isCamera {
			oc.camera.total += 1
			if moving {
				oc.camera.moving += 1
			} else {
				oc.camera.stationary += 1
			}
		}

		if isCamera && isLabel {
			oc.cameraLabel.total += 1
			if moving {
				oc.cameraLabel.moving += 1
			} else {
				oc.cameraLabel.stationary += 1
			}
		}
	}

	return oc
}
