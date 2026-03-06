package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	kafkaclient "switchboard/internal/messaging/kafka"

	"github.com/IBM/sarama"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

const kafkaTopic = "switchboard.events"

type KafkaHandler struct {
	producer sarama.SyncProducer
	nc       *nats.Conn
	log      zerolog.Logger
}

func NewKafkaHandler(producer sarama.SyncProducer, nc *nats.Conn, log zerolog.Logger) *KafkaHandler {
	return &KafkaHandler{producer: producer, nc: nc, log: log}
}

// POST /kafka/produce — produce un mensaje desde el browser
func (h *KafkaHandler) Produce(w http.ResponseWriter, r *http.Request) {
	topic   := r.FormValue("topic")
	key     := r.FormValue("key")
	payload := r.FormValue("payload")

	if topic == "" {
		topic = kafkaTopic
	}
	if payload == "" {
		payload = "{}"
	}
	if !isValidJSON(payload) {
		http.Error(w, "payload must be valid JSON", http.StatusBadRequest)
		return
	}

	var keyBytes []byte
	if key != "" {
		keyBytes = []byte(key)
	}

	partition, offset, err := kafkaclient.Publish(h.producer, topic, keyBytes, []byte(payload))
	if err != nil {
		h.log.Error().Err(err).Str("topic", topic).Msg("kafka produce")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="kafka-row error">
  <span class="text-ctp-red font-mono text-xs">ERR</span>
  <span class="text-ctp-overlay0 font-mono text-xs">%s</span>
  <span class="text-ctp-red font-mono text-xs">%s</span>
</div>`, topic, err.Error())
		return
	}

	h.log.Info().
		Str("topic", topic).
		Int32("partition", partition).
		Int64("offset", offset).
		Msg("kafka produced")

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="kafka-row">
  <span class="text-ctp-peach font-mono text-xs font-bold">PUB</span>
  <span class="text-ctp-mauve font-mono text-xs">%s</span>
  <span class="text-ctp-text font-mono text-xs flex-1">%s</span>
  <span class="text-ctp-overlay0 font-mono text-xs">p:%d o:%d</span>
  <span class="text-ctp-overlay0 font-mono text-xs">%s</span>
</div>`,
		topic, payload, partition, offset,
		time.Now().Format("15:04:05"),
	)
}

// GET /kafka/results — SSE stream de resultados del processor via NATS
func (h *KafkaHandler) Results(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "event: connected\ndata: listening for processor results\n\n")
	flusher.Flush()

	h.log.Info().Msg("kafka sse: client subscribed to results")

	msgCh := make(chan *nats.Msg, 64)
	sub, err := h.nc.ChanSubscribe("switchboard.processed", msgCh)
	if err != nil {
		h.log.Error().Err(err).Msg("nats subscribe error")
		return
	}
	defer sub.Unsubscribe()

	for {
		select {
		case msg := <-msgCh:
			var result ProcessedResult
			if err := json.Unmarshal(msg.Data, &result); err != nil {
				continue
			}
			fmt.Fprintf(w,
				"event: result\ndata: <div class=\"kafka-row result\">"+
					"<span class=\"text-ctp-green font-mono text-xs font-bold\">OK</span>"+
					"<span class=\"text-ctp-mauve font-mono text-xs\">%s</span>"+
					"<span class=\"text-ctp-text font-mono text-xs flex-1\">id:%d</span>"+
					"<span class=\"text-ctp-overlay0 font-mono text-xs\">%s</span>"+
					"</div>\n\n",
				result.Topic, result.EventID,
				time.Now().Format("15:04:05"),
			)
			flusher.Flush()

		case <-r.Context().Done():
			h.log.Info().Msg("kafka sse: client disconnected")
			return
		}
	}
}

type ProcessedResult struct {
	EventID int64  `json:"event_id"`
	Topic   string `json:"topic"`
	Source  string `json:"source"`
}

func isValidJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}