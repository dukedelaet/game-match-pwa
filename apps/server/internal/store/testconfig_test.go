package store

import "gamematch/internal/config"

// testConfig is the minimum config a store needs in tests.
func testConfig(dir string) config.Config {
	return config.Config{Addr: "127.0.0.1:0", DataDir: dir, Env: "test"}
}
