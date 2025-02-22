package util

import (
	"context"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/config"
	"github.com/tweag/credential-helper/logging"
)

func Configure(ctx context.Context, helperFactory api.HelperFactory, configReader config.ConfigReader, uri string) (context.Context, api.Helper) {
	cfg, err := configReader.Read()
	if err == nil {
		logging.Debugf("found config file and choosing helper from it")
		helperFactory = func(uri string) (api.Helper, error) {
			helper, helperConfig, err := cfg.FindHelper(uri)
			if err != nil {
				return nil, err
			}
			if len(helperConfig) > 0 {
				ctx = context.WithValue(ctx, api.HelperConfigKey, helperConfig)
			}
			return helper, nil
		}
	} else if err != config.ErrConfigNotFound {
		logging.Fatalf("reading config: %v", err)
	}

	authenticator, err := helperFactory(uri)
	if err != nil {
		logging.Fatalf("%v", err)
	}

	return ctx, authenticator
}
