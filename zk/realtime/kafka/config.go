package kafka

type KafkaConfig struct {
	BootstrapServers []string
	BlockTopic       string
	TxTopic          string
	ErrorTopic       string
	ClientID         string
	GroupID          string
}
