package main

import (
	"github.com/rs/zerolog/log"

	"github.com/futuretea/http-host-proxy/pkg/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("http-host-proxy failed")
	}
}
