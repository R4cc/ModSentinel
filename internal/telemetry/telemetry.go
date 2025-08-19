package telemetry

import "github.com/rs/zerolog/log"

// Event logs a telemetry event with optional fields. Sensitive values should be omitted by callers.
func Event(name string, fields map[string]string) {
	e := log.Info().Str("event", name)
	for k, v := range fields {
		e = e.Str(k, v)
	}
	e.Msg("telemetry")
}
