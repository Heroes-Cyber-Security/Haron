package integration

import "os"

func mustGetEnv(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		panic("missing " + key)
	}
	return value
}
