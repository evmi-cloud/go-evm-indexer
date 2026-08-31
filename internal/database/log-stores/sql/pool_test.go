package sql_store

import (
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

// The pool bounds protect managed databases with low connection caps: every
// store instance gets its own pool, so each one must stay small.
func TestInitBoundsThePool(t *testing.T) {
	cases := []struct {
		name    string
		config  map[string]string
		wantMax int
	}{
		{"defaults", map[string]string{}, 10},
		{"override", map[string]string{"maxOpenConns": "3"}, 3},
		{"garbage falls back", map[string]string{"maxOpenConns": "lots"}, 10},
		{"zero falls back", map[string]string{"maxOpenConns": "0"}, 10},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewSQLStore("sqlite", zerolog.Nop())
			if err != nil {
				t.Fatal(err)
			}
			tc.config["dsn"] = filepath.Join(t.TempDir(), "store.db")
			if err := s.Init(tc.config); err != nil {
				t.Fatal(err)
			}
			sqlDB, err := s.db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if got := sqlDB.Stats().MaxOpenConnections; got != tc.wantMax {
				t.Fatalf("MaxOpenConnections = %d, want %d", got, tc.wantMax)
			}
		})
	}
}
