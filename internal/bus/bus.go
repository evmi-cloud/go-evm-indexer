package internal_bus

import (
	"github.com/mustafaturan/bus/v3"
	"github.com/mustafaturan/monoton/v2"
	"github.com/mustafaturan/monoton/v2/sequencer"
)

const (
	NewLogTopic         string = "logs.new"
	EnableSourceTopic   string = "source.enable"
	DisableSourceTopic  string = "source.disable"
	ShutdownSignalTopic string = "signal.shutwown"
)

func InitializeBus() *bus.Bus {
	// Create message bus
	// configure id generator (it doesn't have to be monoton)
	node := uint64(1)
	initialTime := uint64(1577865600000) // set 2020-01-01 PST as initial time
	m, err := monoton.New(sequencer.NewMillisecond(), node, initialTime)
	if err != nil {
		panic(err)
	}

	// init an id generator
	var idGenerator bus.Next = m.Next

	// create a new bus instance
	b, err := bus.NewBus(idGenerator)
	if err != nil {
		panic(err)
	}

	b.RegisterTopics(NewLogTopic)
	b.RegisterTopics(EnableSourceTopic)
	b.RegisterTopics(DisableSourceTopic)
	b.RegisterTopics(ShutdownSignalTopic)

	return b
}
