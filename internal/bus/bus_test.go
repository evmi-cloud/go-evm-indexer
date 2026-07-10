package internal_bus

import (
	"context"
	"testing"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/mustafaturan/bus/v3"
)

// The source-update stream relies on the topic being registered and delivering
// EvmLogSource values; an unregistered topic makes Emit fail and updates vanish.
func TestSourceUpdateTopicRoundTrip(t *testing.T) {
	b := InitializeBus()

	got := make(chan evmi_database.EvmLogSource, 1)
	b.RegisterHandler("test", bus.Handler{
		Matcher: SourceUpdateTopic,
		Handle: func(_ context.Context, e bus.Event) {
			if s, ok := e.Data.(evmi_database.EvmLogSource); ok {
				got <- s
			}
		},
	})

	src := evmi_database.EvmLogSource{Type: "CONTRACT", SyncBlock: 42}
	if err := b.Emit(context.Background(), SourceUpdateTopic, src); err != nil {
		t.Fatalf("emit failed (topic not registered?): %v", err)
	}

	// bus.Emit dispatches handlers synchronously, so the value is already queued.
	select {
	case s := <-got:
		if s.SyncBlock != 42 || s.Type != "CONTRACT" {
			t.Errorf("delivered wrong source: %+v", s)
		}
	default:
		t.Fatal("handler did not receive the source update")
	}
}
