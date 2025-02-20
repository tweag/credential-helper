package helperconfig

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/tweag/credential-helper/api"
)

func FromContext[T any](ctx context.Context, config T) (T, error) {
	rawConfig, ok := ctx.Value(api.HelperConfigKey).([]byte)
	if !ok {
		return config, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(rawConfig))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&config)
	return config, err
}
