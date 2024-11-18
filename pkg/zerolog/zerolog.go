package pkgzerolog

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
)

func SetLogLevel(logLevel string) {
	switch logLevel {
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	}
}

func SetupZeroLog(logLevel string) *zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "3:04PM"}
	consoleWriter.FormatLevel = func(i interface{}) string {
		if ll, ok := i.(string); ok {
			switch ll {
			case "debug":
				return "\033[36mDBG\033[0m" // Cyan
			case "info":
				return "\033[32mINF\033[0m" // Green
			case "warn":
				return "\033[33mWRN\033[0m" // Yellow
			case "error":
				return "\033[31mERR\033[0m" // Red
			case "trace":
				return "\033[35mTRC\033[0m" // Magenta
			default:
				return ll
			}
		}
		return "???"
	}
	consoleWriter.FormatMessage = func(i interface{}) string {
		return fmt.Sprintf("\033[97m%s\033[0m", i) // White
	}
	consoleWriter.FormatFieldName = func(i interface{}) string {
		return fmt.Sprintf("\033[94m%s\033[0m=", i) // Blue
	}
	consoleWriter.FormatFieldValue = func(i interface{}) string {
		return fmt.Sprintf("\033[96m%s\033[0m", i) // Light Cyan
	}

	_logger := zerolog.New(consoleWriter).With().Timestamp().Logger()
	SetLogLevel(logLevel)
	return &_logger
}
