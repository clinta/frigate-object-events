Add moving and stationary detectors for all labels in frigate to home assistant.

This daemon looks for frigate events from MQTT and track when an event's motion starts and ends, publishing a moving and stationary count for each label in frigate.

It also publishes the sensors for automatic discovery in home assistant.

## Install

1: Download frigate-object-events from releases, or compile with `go build .` and put in `/usr/local/bin`
2: Put the systemd [unit](frigate-object-events.service) in `/etc/systemd/system`
3: Reload your units (`systemctl daemon-reload`) and start the service (`systemctl start frigate-object-events.service`)