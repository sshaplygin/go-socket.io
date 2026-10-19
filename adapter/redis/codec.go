package redis

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/sshaplygin/go-socket.io/logger"
)

func (bc *broadcast) onMessage(channel string, msg []byte) error {
	// Only the final delimiter separates the opaque namespace from the UID.
	prefix := strings.TrimSuffix(bc.key, bc.uid)
	uid, ok := strings.CutPrefix(channel, prefix)
	if !ok || uid == "" || strings.Contains(uid, "#") {
		return nil
	}
	if bc.uid == uid {
		return nil
	}

	var bcMessage map[string]json.RawMessage
	if err := json.Unmarshal(msg, &bcMessage); err != nil {
		return errors.New("invalid broadcast message")
	}
	rawArgs, hasArgs := bcMessage["args"]
	rawOpts, hasOpts := bcMessage["opts"]
	if len(bcMessage) != 2 || !hasArgs || !hasOpts {
		logger.Log.Warn("socketio: unsupported redis broadcast message")
		return errors.New("unsupported broadcast message")
	}
	var opts, args []interface{}
	if json.Unmarshal(rawOpts, &opts) != nil || json.Unmarshal(rawArgs, &args) != nil {
		return errors.New("invalid broadcast message")
	}
	if len(opts) != 2 {
		logger.Log.Warn("socketio: unsupported redis broadcast message")
		return errors.New("unsupported broadcast message")
	}

	room, ok := opts[0].(string)
	if !ok {
		return errors.New("invalid room")
	}

	event, ok := opts[1].(string)
	if !ok {
		return errors.New("invalid event")
	}

	if room != "" {
		bc.Memory.Send(room, event, args...)
	} else {
		bc.Memory.SendAll(event, args...)
	}

	return nil
}

// do runs one command on a pooled publishing connection.
func (bc *broadcast) do(cmd string, args ...interface{}) (interface{}, error) {
	c := bc.pub.Get()
	defer func() { _ = c.Close() }()
	return c.Do(cmd, args...)
}

func (bc *broadcast) publish(channel string, msg interface{}) {
	resJSON, err := json.Marshal(msg)
	if err != nil {
		return
	}

	_, err = bc.do("PUBLISH", channel, resJSON)
	if err != nil {
		return
	}
}

func (bc *broadcast) publishMessage(room string, event string, args ...interface{}) {
	opts := make([]interface{}, 2)
	opts[0] = room
	opts[1] = event

	bcMessage := map[string][]interface{}{
		"opts": opts,
		"args": args,
	}
	bcMessageJSON, err := json.Marshal(bcMessage)
	if err != nil {
		return
	}

	_, err = bc.do("PUBLISH", bc.key, bcMessageJSON)
	if err != nil {
		return
	}
}
