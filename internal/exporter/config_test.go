package exporter

import "testing"

func TestValidatePluginConfig(t *testing.T) {
	schema := []byte(`[
		{"name":"dsn","type":"string","required":true},
		{"name":"decimals","type":"number","required":false},
		{"name":"dryRun","type":"bool","required":false}
	]`)

	cases := []struct {
		name    string
		schema  []byte
		config  string
		wantErr bool
	}{
		{"no schema passes anything", nil, `{"whatever": 1}`, false},
		{"valid full config", schema, `{"dsn":"postgres://x","decimals":6,"dryRun":true}`, false},
		{"valid minimal (only required)", schema, `{"dsn":"postgres://x"}`, false},
		{"extra keys allowed", schema, `{"dsn":"x","extra":"ok"}`, false},
		{"missing required", schema, `{"decimals":6}`, true},
		{"number field given a string", schema, `{"dsn":"x","decimals":"6"}`, true},
		{"bool field given a number", schema, `{"dsn":"x","dryRun":1}`, true},
		{"invalid json config", schema, `{not json}`, true},
	}

	for _, c := range cases {
		err := ValidatePluginConfig(c.schema, []byte(c.config))
		if (err != nil) != c.wantErr {
			t.Errorf("%s: got err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}
}
