package flaggy

import (
	"flag"
	"os"
	"time"
)

func GetEnvDurationVar(
	envVarName string,
	flagName string,
	description string,
	defaultVal time.Duration,
) *time.Duration {
	envVal := os.Getenv(envVarName)
	if len(envVal) > 0 {
		durVal, err := time.ParseDuration(envVal)
		if err == nil {
			return &durVal
		}
	}

	return flag.Duration(flagName, defaultVal, description)
}
