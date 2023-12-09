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
	"syscall"
	"time"

	"github.com/eclipse/paho.golang/paho"
)

type UnixTime struct {
	time.Time
}

func (u *UnixTime) UnmarshalJSON(b []byte) error {
	var timeFloat float64
	err := json.Unmarshal(b, &timeFloat)
	if err != nil {
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
	//SnapshotTime    float64 `json:"snapshot_time"`
	Label    string `json:"label"`
	SubLabel string `json:"sub_label"`
	//TopScore        float64 `json:"top_score"`
	//FalsePositive   bool    `json:"false_positive"`
	StartTime UnixTime `json:"start_time"`
	EndTime   UnixTime `json:"end_time"`
	//Score           float64 `json:"score"`
	//Box             []int   `json:"box"`
	//Area            int     `json:"area"`
	//Ratio           float64 `json:"ratio"`
	//Region          []int   `json:"region"`
	Stationary bool `json:"stationary"`
	//MotionlessCount int     `json:"motionless_count"`
	//PositionChanges int     `json:"position_changes"`
	CurrentZones []string `json:"current_zones"`
	//EnteredZones    []any   `json:"entered_zones"`
	//HasClip         bool    `json:"has_clip"`
	//HasSnapshot     bool    `json:"has_snapshot"`
}

func main() {
	server := flag.String("server", "127.0.0.1:1883", "The MQTT server to connect to ex: 127.0.0.1:1883")
	topic := flag.String("topic", "#", "Topic to subscribe to")
	qos := flag.Int("qos", 0, "The QoS to subscribe to messages at")
	clientid := flag.String("clientid", "", "A clientid for the connection")
	username := flag.String("username", "", "A username to authenticate to the MQTT server")
	password := flag.String("password", "", "Password to match username")
	flag.Parse()

	logger := log.New(os.Stdout, "SUB: ", log.LstdFlags)

	msgChan := make(chan *paho.Publish)

	conn, err := net.Dial("tcp", *server)
	if err != nil {
		log.Fatalf("Failed to connect to %s: %s", *server, err)
	}

	c := paho.NewClient(paho.ClientConfig{
		Router: paho.NewStandardRouterWithDefault(func(m *paho.Publish) {
			msgChan <- m
		}),
		Conn: conn,
	})
	c.SetDebugLogger(logger)
	c.SetErrorLogger(logger)

	cp := &paho.Connect{
		KeepAlive:  30,
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

	ca, err := c.Connect(context.Background(), cp)
	if err != nil {
		log.Fatalln(err)
	}
	if ca.ReasonCode != 0 {
		log.Fatalf("Failed to connect to %s : %d - %s", *server, ca.ReasonCode, ca.Properties.ReasonString)
	}

	fmt.Printf("Connected to %s\n", *server)

	ic := make(chan os.Signal, 1)
	signal.Notify(ic, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ic
		fmt.Println("signal received, exiting")
		if c != nil {
			d := &paho.Disconnect{ReasonCode: 0}
			c.Disconnect(d)
		}
		os.Exit(0)
	}()

	sa, err := c.Subscribe(context.Background(), &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: *topic, QoS: byte(*qos)},
		},
	})
	if err != nil {
		log.Fatalln(err)
	}
	if sa.Reasons[0] != byte(*qos) {
		log.Fatalf("Failed to subscribe to %s : %d", *topic, sa.Reasons[0])
	}
	log.Printf("Subscribed to %s", *topic)

	cameras := make(labelsByCamera)

	for m := range msgChan {
		var fm FrigateMessage
		if err := json.Unmarshal(m.Payload, &fm); err != nil {
			log.Fatalf("Failed to unmarshal message: %e", err)
		}
		labels, ok := cameras[fm.After.Camera]
		if !ok {
			labels = make(eventsByLabel)
			cameras[fm.After.Camera] = labels
		}
		events, ok := labels[fm.After.Label]
		if !ok {
			events = make(eventsById)
			labels[fm.After.Label] = events
		}

		if fm.After.EndTime.Unix() != 0 {
			log.Printf("Deleting ID: %s", fm.After.ID)
			//spew.Dump(fm)
			delete(events, fm.After.ID)
		} else {
			events[fm.After.ID] = &fm.After
		}

		for camera, labels := range cameras {
			log.Printf("%s:", camera)
			for label, events := range labels {
				log.Printf("  %s:", label)
				moving, stationary, total := 0, 0, 0
				for _, event := range events {
					total += 1
					if event.Stationary {
						stationary += 1
					} else {
						moving += 1
					}
				}
				log.Printf("    moving: %d", moving)
				log.Printf("    stationary: %d", stationary)
				log.Printf("    total: %d", total)
				if _, err := c.Publish(context.TODO(), &paho.Publish{
					Topic:   "frigate_occupancy/test",
					Payload: []byte("foobar"),
				}); err != nil {
					log.Fatalf("Error publishing: %e", err)
				}
			}
		}

		//spew.Dump(cameras)
		//log.Println("Received message:", string(m.Payload))
	}
}

type labelsByCamera = map[string]eventsByLabel

type eventsByLabel = map[string]eventsById

type eventsById = map[string]*After
